package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sing-box-web-panel/internal/domain"
	svcclient "sing-box-web-panel/internal/services/client"
	svcnode "sing-box-web-panel/internal/services/node"
)

type ClientHandler struct {
	svc        *svcclient.Service
	nodes      *svcnode.Service
	subBaseURL string
	log        *slog.Logger
}

func NewClientHandler(svc *svcclient.Service, subBaseURL string, log *slog.Logger, nodes ...*svcnode.Service) *ClientHandler {
	h := &ClientHandler{svc: svc, subBaseURL: strings.TrimRight(subBaseURL, "/"), log: log}
	if len(nodes) > 0 {
		h.nodes = nodes[0]
	}
	return h
}

func (h *ClientHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/clients", h.List)
	mux.HandleFunc("POST /api/clients", h.Create)
	mux.HandleFunc("GET /api/clients/{id}", h.Get)
	mux.HandleFunc("PUT /api/clients/{id}", h.Update)
	mux.HandleFunc("DELETE /api/clients/{id}", h.Delete)
	mux.HandleFunc("POST /api/clients/{id}/reset-traffic", h.ResetTraffic)
	mux.HandleFunc("POST /api/clients/{id}/status", h.SetStatus)
	mux.HandleFunc("POST /api/nodes/{id}/clients", h.CreateOnNode)
}

func (h *ClientHandler) subURL(token string) string {
	if h.subBaseURL == "" {
		return "/sub/" + token
	}
	return h.subBaseURL + "/sub/" + token
}

type clientDTO struct {
	ID                 string `json:"id"`
	NodeID             string `json:"nodeId,omitempty"`
	RemoteID           string `json:"remoteId,omitempty"`
	Name               string   `json:"name"`
	UUID               string   `json:"uuid"`
	InboundID          string   `json:"inboundId"`
	InboundIDs         []string `json:"inboundIds"`
	UsedDown           int64    `json:"usedDown"`
	UsedUp             int64    `json:"usedUp"`
	TotalQuota         int64    `json:"totalQuota"`
	Expiry             string   `json:"expiry"`
	Status             string   `json:"status"`
	Subscription       string   `json:"subscription"`
	SubToken           string `json:"subToken,omitempty"`
	Enabled            bool   `json:"enabled"`
	StartAfterFirstUse bool   `json:"startAfterFirstUse"`
	Online             bool   `json:"online"`
}

func (h *ClientHandler) toDTO(c *domain.Client) clientDTO {
	dto := clientDTO{
		ID:                 strconv.FormatInt(c.ID, 10),
		RemoteID:           c.RemoteID,
		Name:               c.Name,
		UUID:               c.UUID,
		InboundID:          strconv.FormatInt(c.InboundID, 10),
		InboundIDs:         formatInboundIDs(c),
		UsedDown:           c.UsedDown,
		UsedUp:             c.UsedUp,
		TotalQuota:         c.TotalQuota,
		Expiry:             formatTimePtr(c.Expiry),
		Status:             string(c.Status),
		Subscription:       h.subURL(c.SubToken),
		SubToken:           c.SubToken,
		Enabled:            c.Enabled,
		StartAfterFirstUse: c.StartAfterFirstUse,
		Online:             isOnline(c.LastUsedAt),
	}
	if c.NodeID != nil {
		dto.NodeID = strconv.FormatInt(*c.NodeID, 10)
	}
	return dto
}

// List godoc
//
//	@Summary	List clients
//	@Tags		clients
//	@Produce	json
//	@Security	BearerAuth
//	@Param		inboundId	query	int	false	"Filter by inbound ID"
//	@Success	200	{array}	clientDTO
//	@Router		/clients [get]
func (h *ClientHandler) List(w http.ResponseWriter, r *http.Request) {
	var filter *int64
	if q := r.URL.Query().Get("inboundId"); q != "" {
		id, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid inboundId"})
			return
		}
		filter = &id
	}
	clients, err := h.svc.List(r.Context(), filter)
	if err != nil {
		writeServiceError(w, h.log, "list clients", err)
		return
	}
	out := make([]clientDTO, 0, len(clients))
	for i := range clients {
		out = append(out, h.toDTO(&clients[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

type createClientRequest struct {
	NodeID             string   `json:"nodeId,omitempty"`
	Name               string   `json:"name"`
	InboundID          string   `json:"inboundId"`
	InboundIDs         []string `json:"inboundIds,omitempty"`
	TotalQuota         int64    `json:"totalQuota"`
	Expiry             string   `json:"expiry"`
	StartAfterFirstUse bool     `json:"startAfterFirstUse"`
}

// Create godoc
//
//	@Summary	Create a client
//	@Tags		clients
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		createClientRequest	true	"Client"
//	@Success	201		{object}	clientDTO
//	@Router		/clients [post]
func (h *ClientHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createClientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, ok := req.toInput(w)
	if !ok {
		return
	}
	if req.NodeID != "" {
		nodeID, ok := parsePositiveID(w, req.NodeID, "invalid nodeId")
		if !ok {
			return
		}
		h.createRemote(w, r, nodeID, in)
		return
	}
	c, err := h.svc.Create(r.Context(), in)
	if err != nil {
		writeServiceError(w, h.log, "create client", err)
		return
	}
	writeJSON(w, http.StatusCreated, h.toDTO(c))
}

func (h *ClientHandler) CreateOnNode(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid nodeId"})
		return
	}
	var req createClientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.NodeID != "" && !matchesNodeID(w, req.NodeID, nodeID) {
		return
	}
	in, ok := req.toInput(w)
	if !ok {
		return
	}
	h.createRemote(w, r, nodeID, in)
}

func (h *ClientHandler) createRemote(w http.ResponseWriter, r *http.Request, nodeID int64, in svcclient.CreateInput) {
	if h.nodes == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node mutations are not configured"})
		return
	}
	c, err := h.nodes.CreateClient(r.Context(), nodeID, in)
	if err != nil {
		writeServiceError(w, h.log, "create remote client", err)
		return
	}
	writeJSON(w, http.StatusCreated, h.toDTO(c))
}

func (req createClientRequest) toInput(w http.ResponseWriter) (svcclient.CreateInput, bool) {
	ids, ok := parseInboundIDs(w, req.InboundIDs, req.InboundID)
	if !ok {
		return svcclient.CreateInput{}, false
	}
	expiry, err := parseTimePtr(req.Expiry)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid expiry"})
		return svcclient.CreateInput{}, false
	}
	return svcclient.CreateInput{
		Name:               req.Name,
		InboundID:          ids[0],
		InboundIDs:         ids,
		TotalQuota:         req.TotalQuota,
		Expiry:             expiry,
		StartAfterFirstUse: req.StartAfterFirstUse,
	}, true
}

// Get godoc
//
//	@Summary	Get a client
//	@Tags		clients
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		int	true	"Client ID"
//	@Success	200	{object}	clientDTO
//	@Router		/clients/{id} [get]
func (h *ClientHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	c, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "get client", err)
		return
	}
	writeJSON(w, http.StatusOK, h.toDTO(c))
}

type updateClientRequest struct {
	NodeID             *string   `json:"nodeId,omitempty"`
	Name               *string   `json:"name"`
	InboundID          *string   `json:"inboundId"`
	InboundIDs         *[]string `json:"inboundIds"`
	TotalQuota         *int64    `json:"totalQuota"`
	Expiry             *string   `json:"expiry"`
	Status             *string   `json:"status"`
	StartAfterFirstUse *bool     `json:"startAfterFirstUse"`
}

// Update godoc
//
//	@Summary	Update a client
//	@Tags		clients
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		int					true	"Client ID"
//	@Param		request	body		updateClientRequest	true	"Patch"
//	@Success	200		{object}	clientDTO
//	@Router		/clients/{id} [put]
func (h *ClientHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	var req updateClientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, ok := req.toInput(w)
	if !ok {
		return
	}
	existing, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "get client", err)
		return
	}
	if existing.NodeID != nil {
		if h.nodes == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node mutations are not configured"})
			return
		}
		if req.NodeID != nil && *req.NodeID != "" && !matchesNodeID(w, *req.NodeID, *existing.NodeID) {
			return
		}
		c, err := h.nodes.UpdateClient(r.Context(), id, in)
		if err != nil {
			writeServiceError(w, h.log, "update remote client", err)
			return
		}
		writeJSON(w, http.StatusOK, h.toDTO(c))
		return
	}
	if req.NodeID != nil && *req.NodeID != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot move client between nodes"})
		return
	}
	c, err := h.svc.Update(r.Context(), id, in)
	if err != nil {
		writeServiceError(w, h.log, "update client", err)
		return
	}
	writeJSON(w, http.StatusOK, h.toDTO(c))
}

func (req updateClientRequest) toInput(w http.ResponseWriter) (svcclient.UpdateInput, bool) {
	in := svcclient.UpdateInput{
		Name:               req.Name,
		TotalQuota:         req.TotalQuota,
		StartAfterFirstUse: req.StartAfterFirstUse,
	}
	if req.InboundIDs != nil {
		ids, ok := parseInboundIDs(w, *req.InboundIDs, "")
		if !ok {
			return svcclient.UpdateInput{}, false
		}
		in.InboundIDs = &ids
	} else if req.InboundID != nil {
		inboundID, err := strconv.ParseInt(*req.InboundID, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid inboundId"})
			return svcclient.UpdateInput{}, false
		}
		in.InboundID = &inboundID
	}
	if req.Expiry != nil {
		expiry, err := parseTimePtr(*req.Expiry)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid expiry"})
			return svcclient.UpdateInput{}, false
		}
		in.Expiry = expiry
	}
	if req.Status != nil {
		status := domain.ClientStatus(*req.Status)
		in.Status = &status
	}
	return in, true
}

// Delete godoc
//
//	@Summary	Delete a client
//	@Tags		clients
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path	int	true	"Client ID"
//	@Success	200	{object}	map[string]string
//	@Router		/clients/{id} [delete]
func (h *ClientHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	existing, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "get client", err)
		return
	}
	if existing.NodeID != nil {
		if h.nodes == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node mutations are not configured"})
			return
		}
		if err := h.nodes.DeleteClient(r.Context(), id); err != nil {
			writeServiceError(w, h.log, "delete remote client", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		writeServiceError(w, h.log, "delete client", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// ResetTraffic godoc
//
//	@Summary	Reset a client's traffic counters
//	@Tags		clients
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		int	true	"Client ID"
//	@Success	200	{object}	clientDTO
//	@Router		/clients/{id}/reset-traffic [post]
func (h *ClientHandler) ResetTraffic(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	existing, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "get client", err)
		return
	}
	if existing.NodeID != nil {
		if h.nodes == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node mutations are not configured"})
			return
		}
		c, err := h.nodes.ResetClientTraffic(r.Context(), id)
		if err != nil {
			writeServiceError(w, h.log, "reset remote client traffic", err)
			return
		}
		writeJSON(w, http.StatusOK, h.toDTO(c))
		return
	}
	c, err := h.svc.ResetTraffic(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "reset client traffic", err)
		return
	}
	writeJSON(w, http.StatusOK, h.toDTO(c))
}

type setStatusRequest struct {
	Status string `json:"status"`
}

// SetStatus godoc
//
//	@Summary	Set a client's status
//	@Tags		clients
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		int					true	"Client ID"
//	@Param		request	body		setStatusRequest	true	"Status"
//	@Success	200		{object}	clientDTO
//	@Router		/clients/{id}/status [post]
func (h *ClientHandler) SetStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	var req setStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	existing, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "get client", err)
		return
	}
	if existing.NodeID != nil {
		if h.nodes == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node mutations are not configured"})
			return
		}
		c, err := h.nodes.SetClientStatus(r.Context(), id, domain.ClientStatus(req.Status))
		if err != nil {
			writeServiceError(w, h.log, "set remote client status", err)
			return
		}
		writeJSON(w, http.StatusOK, h.toDTO(c))
		return
	}
	c, err := h.svc.SetStatus(r.Context(), id, domain.ClientStatus(req.Status))
	if err != nil {
		writeServiceError(w, h.log, "set client status", err)
		return
	}
	writeJSON(w, http.StatusOK, h.toDTO(c))
}

const onlineThreshold = time.Second * 5

func isOnline(lastUsedAt *time.Time) bool {
	return lastUsedAt != nil && time.Since(*lastUsedAt) < onlineThreshold
}

// parseInboundIDs resolves the inbound id set from a request, accepting the new
// inboundIds array and falling back to the legacy single inboundId. At least one
// id is required.
func parseInboundIDs(w http.ResponseWriter, ids []string, legacy string) ([]int64, bool) {
	raw := ids
	if len(raw) == 0 && legacy != "" {
		raw = []string{legacy}
	}
	out := make([]int64, 0, len(raw))
	for _, s := range raw {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid inboundId"})
			return nil, false
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one inbound is required"})
		return nil, false
	}
	return out, true
}

// formatInboundIDs renders the client's inbound set as strings, falling back to
// the primary InboundID when the set is empty.
func formatInboundIDs(c *domain.Client) []string {
	ids := c.InboundIDs
	if len(ids) == 0 {
		ids = []int64{c.InboundID}
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, strconv.FormatInt(id, 10))
	}
	return out
}
