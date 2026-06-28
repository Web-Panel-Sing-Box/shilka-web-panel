package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
	"sing-box-web-panel/internal/services/sublink"
)

// ClientByToken resolves a client from its subscription token.
type ClientByToken interface {
	GetBySubToken(ctx context.Context, token string) (*domain.Client, error)
	GetByID(ctx context.Context, id int64) (*domain.Client, error)
}

// InboundByID resolves an inbound by id.
type InboundByID interface {
	GetByID(ctx context.Context, id int64) (*domain.Inbound, error)
}

// SettingGetter reads a single panel setting.
type SettingGetter interface {
	Get(ctx context.Context, key string) (string, error)
}

type SubscriptionHandler struct {
	clients     ClientByToken
	inbounds    InboundByID
	settings    SettingGetter
	subBaseURL  string
	defaultHost string
	log         *slog.Logger
}

func NewSubscriptionHandler(clients ClientByToken, inbounds InboundByID, settings SettingGetter, subBaseURL, defaultHost string, log *slog.Logger) *SubscriptionHandler {
	return &SubscriptionHandler{
		clients:     clients,
		inbounds:    inbounds,
		settings:    settings,
		subBaseURL:  strings.TrimRight(subBaseURL, "/"),
		defaultHost: defaultHost,
		log:         log,
	}
}

func (h *SubscriptionHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /sub/{token}", h.Serve)                   // public
	mux.HandleFunc("GET /api/subscription/{token}", h.Serve)      // public
	mux.HandleFunc("GET /api/subscription/{token}/meta", h.Meta)  // public (subscription page)
	mux.HandleFunc("GET /api/clients/{id}/links", h.Links)        // authenticated
}

// wantsHTML reports whether the request comes from a browser navigation that
// should see the human-facing subscription page instead of raw config.
func wantsHTML(r *http.Request) bool {
	if r.URL.Query().Get("format") != "" {
		return false // explicit format → always return config
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// Serve godoc
//
//	@Summary	Fetch a subscription (public)
//	@Description	Returns the client's connection config. ?format=base64|plain|json (default base64).
//	@Tags		subscription
//	@Produce	plain
//	@Param		token	path	string	true	"Subscription token"
//	@Param		format	query	string	false	"base64 | plain | json"
//	@Success	200	{string}	string
//	@Failure	404	{object}	map[string]string
//	@Router		/subscription/{token} [get]
func (h *SubscriptionHandler) Serve(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	// Browsers opening the subscription link get the human-facing page (SPA
	// hash route); VPN clients (no text/html Accept) fall through to config.
	if strings.HasPrefix(r.URL.Path, "/sub/") && wantsHTML(r) {
		http.Redirect(w, r, "/#/sub/"+token, http.StatusFound)
		return
	}

	client, err := h.clients.GetBySubToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		h.log.Error("subscription lookup", slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !client.Enabled || client.Status != domain.ClientStatusActive {
		http.Error(w, "subscription disabled", http.StatusForbidden)
		return
	}

	inbounds := h.resolveInbounds(r.Context(), client)
	if len(inbounds) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	format := r.URL.Query().Get("format")
	result, err := sublink.RenderMulti(format, inbounds, client, h.resolveHost(r.Context(), r))
	if err != nil {
		if errors.Is(err, sublink.ErrNaiveJSONRequiresTrustedTLS) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		h.log.Error("render subscription", slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Profile-Update-Interval", "12")
	w.WriteHeader(http.StatusOK)
	w.Write(result.Body)
}

type subscriptionLinkDTO struct {
	Label    string `json:"label"`
	URL      string `json:"url"`
	Protocol string `json:"protocol"`
}

type subscriptionMetaDTO struct {
	Name            string                `json:"name"`
	Used            int64                 `json:"used"`
	Total           int64                 `json:"total"`
	Expiry          string                `json:"expiry"`
	Online          bool                  `json:"online"`
	SubscriptionURL string                `json:"subscriptionUrl"`
	Links           []subscriptionLinkDTO `json:"links"`
}

// Meta godoc
//
//	@Summary	Subscription metadata for the public subscription page (public)
//	@Tags		subscription
//	@Produce	json
//	@Param		token	path	string	true	"Subscription token"
//	@Success	200	{object}	subscriptionMetaDTO
//	@Failure	404	{object}	map[string]string
//	@Router		/subscription/{token}/meta [get]
func (h *SubscriptionHandler) Meta(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	client, err := h.clients.GetBySubToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		h.log.Error("subscription meta lookup", slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !client.Enabled || client.Status != domain.ClientStatusActive {
		http.Error(w, "subscription disabled", http.StatusForbidden)
		return
	}
	inbounds := h.resolveInbounds(r.Context(), client)
	if len(inbounds) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	host := h.resolveHost(r.Context(), r)
	links := buildLinkDTOs(inbounds, client, host)
	sub := "/sub/" + client.SubToken
	if h.subBaseURL != "" {
		sub = h.subBaseURL + "/sub/" + client.SubToken
	}
	expiry := ""
	if client.Expiry != nil {
		expiry = client.Expiry.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, subscriptionMetaDTO{
		Name:            client.Name,
		Used:            client.UsedUp + client.UsedDown,
		Total:           client.TotalQuota,
		Expiry:          expiry,
		Online:          isOnline(client.LastUsedAt),
		SubscriptionURL: sub,
		Links:           links,
	})
}

type clientLinksDTO struct {
	Link         string                `json:"link"`
	ShareLink    string                `json:"shareLink"`
	Subscription string                `json:"subscription"`
	Links        []subscriptionLinkDTO `json:"links"`
}

// Links godoc
//
//	@Summary	Get a client's share link and subscription URL
//	@Tags		clients
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		int	true	"Client ID"
//	@Success	200	{object}	clientLinksDTO
//	@Router		/clients/{id}/links [get]
func (h *SubscriptionHandler) Links(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	client, err := h.clients.GetByID(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "client links", err)
		return
	}
	inbounds := h.resolveInbounds(r.Context(), client)
	if len(inbounds) == 0 {
		writeServiceError(w, h.log, "client links inbound", repo.ErrNotFound)
		return
	}
	sub := "/sub/" + client.SubToken
	if h.subBaseURL != "" {
		sub = h.subBaseURL + "/sub/" + client.SubToken
	}
	links := buildLinkDTOs(inbounds, client, h.resolveHost(r.Context(), r))
	first := ""
	if len(links) > 0 {
		first = links[0].URL
	}
	writeJSON(w, http.StatusOK, clientLinksDTO{
		Link:         first,
		ShareLink:    first,
		Subscription: sub,
		Links:        links,
	})
}

// resolveInbounds returns every inbound the client is bound to, in order,
// skipping any that no longer exist. Falls back to the legacy primary InboundID.
func (h *SubscriptionHandler) resolveInbounds(ctx context.Context, client *domain.Client) []*domain.Inbound {
	ids := client.InboundIDs
	if len(ids) == 0 {
		ids = []int64{client.InboundID}
	}
	out := make([]*domain.Inbound, 0, len(ids))
	for _, id := range ids {
		ib, err := h.inbounds.GetByID(ctx, id)
		if err != nil {
			continue
		}
		out = append(out, ib)
	}
	return out
}

// buildLinkDTOs builds one share-link DTO per inbound, dropping inbounds whose
// protocol yields no link.
func buildLinkDTOs(inbounds []*domain.Inbound, client *domain.Client, host string) []subscriptionLinkDTO {
	links := make([]subscriptionLinkDTO, 0, len(inbounds))
	for _, ib := range inbounds {
		url := sublink.BuildLink(ib, client, host)
		if url == "" {
			continue
		}
		links = append(links, subscriptionLinkDTO{Label: ib.Remark, URL: url, Protocol: string(ib.Protocol)})
	}
	return links
}

func (h *SubscriptionHandler) resolveHost(ctx context.Context, r *http.Request) string {
	if h.settings != nil {
		if v, err := h.settings.Get(ctx, domain.SettingInboundHost); err == nil && v != "" {
			return v
		}
	}
	if h.defaultHost != "" {
		return h.defaultHost
	}
	return sublink.HostFromRequestHost(r.Host)
}
