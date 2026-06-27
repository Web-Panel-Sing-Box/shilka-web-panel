package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
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
	mux.HandleFunc("POST /api/clients/bulk/delete", h.BulkDelete)
	mux.HandleFunc("POST /api/clients/bulk/set-status", h.BulkSetStatus)
	mux.HandleFunc("POST /api/clients/bulk/reset-traffic", h.BulkResetTraffic)
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

type bulkClientRequest struct {
	IDs    []string `json:"ids"`
	Status string   `json:"status,omitempty"`
}

type bulkClientResultDTO struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type bulkClientResponse struct {
	Results []bulkClientResultDTO `json:"results"`
}

type bulkClientTarget struct {
	rawID string
	id    int64
}

type bulkLocalMutation func(context.Context, []int64) ([]svcclient.BulkResult, error)
type bulkRemoteMutation func(context.Context, int64, []int64) ([]svcclient.BulkResult, error)

// BulkDelete godoc
//
//	@Summary	Delete multiple clients
//	@Tags		clients
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		bulkClientRequest	true	"Client IDs"
//	@Success	200		{object}	bulkClientResponse
//	@Failure	400		{object}	map[string]string
//	@Router		/clients/bulk/delete [post]
func (h *ClientHandler) BulkDelete(w http.ResponseWriter, r *http.Request) {
	targets, ok := decodeBulkClientTargets(w, r)
	if !ok {
		return
	}
	response := h.runBulk(r.Context(), targets, "bulk delete clients", h.svc.BulkDelete, h.remoteBulkDelete)
	writeJSON(w, http.StatusOK, response)
}

// BulkResetTraffic godoc
//
//	@Summary	Reset traffic for multiple clients
//	@Tags		clients
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		bulkClientRequest	true	"Client IDs"
//	@Success	200		{object}	bulkClientResponse
//	@Failure	400		{object}	map[string]string
//	@Router		/clients/bulk/reset-traffic [post]
func (h *ClientHandler) BulkResetTraffic(w http.ResponseWriter, r *http.Request) {
	targets, ok := decodeBulkClientTargets(w, r)
	if !ok {
		return
	}
	response := h.runBulk(r.Context(), targets, "bulk reset client traffic", h.svc.BulkResetTraffic, h.remoteBulkResetTraffic)
	writeJSON(w, http.StatusOK, response)
}

// BulkSetStatus godoc
//
//	@Summary	Set status for multiple clients
//	@Tags		clients
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		bulkClientRequest	true	"Client IDs and status"
//	@Success	200		{object}	bulkClientResponse
//	@Failure	400		{object}	map[string]string
//	@Router		/clients/bulk/set-status [post]
func (h *ClientHandler) BulkSetStatus(w http.ResponseWriter, r *http.Request) {
	var req bulkClientRequest
	targets, ok := decodeBulkClientRequest(w, r, &req)
	if !ok {
		return
	}
	status := domain.ClientStatus(req.Status)
	if status != domain.ClientStatusActive && status != domain.ClientStatusDisabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be active or disabled"})
		return
	}
	response := h.runBulk(
		r.Context(),
		targets,
		"bulk set client status",
		func(ctx context.Context, ids []int64) ([]svcclient.BulkResult, error) {
			return h.svc.BulkSetStatus(ctx, ids, status)
		},
		func(ctx context.Context, nodeID int64, ids []int64) ([]svcclient.BulkResult, error) {
			if h.nodes == nil {
				err := fmt.Errorf("%w: node mutations are not configured", svcnode.ErrValidation)
				return bulkServiceFailures(ids, err), err
			}
			return h.nodes.BulkSetClientStatus(ctx, nodeID, ids, status)
		},
	)
	writeJSON(w, http.StatusOK, response)
}

func decodeBulkClientTargets(w http.ResponseWriter, r *http.Request) ([]bulkClientTarget, bool) {
	var req bulkClientRequest
	return decodeBulkClientRequest(w, r, &req)
}

func decodeBulkClientRequest(w http.ResponseWriter, r *http.Request, req *bulkClientRequest) ([]bulkClientTarget, bool) {
	if !decodeJSON(w, r, req) {
		return nil, false
	}
	if len(req.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids must not be empty"})
		return nil, false
	}
	seen := make(map[int64]struct{}, len(req.IDs))
	targets := make([]bulkClientTarget, 0, len(req.IDs))
	for _, rawID := range req.IDs {
		id, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids must contain positive integers"})
			return nil, false
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		targets = append(targets, bulkClientTarget{rawID: rawID, id: id})
	}
	return targets, true
}

func (h *ClientHandler) runBulk(
	ctx context.Context,
	targets []bulkClientTarget,
	op string,
	localMutation bulkLocalMutation,
	remoteMutation bulkRemoteMutation,
) bulkClientResponse {
	response := bulkClientResponse{Results: make([]bulkClientResultDTO, len(targets))}
	indexByID := make(map[int64]int, len(targets))
	localIDs := make([]int64, 0, len(targets))
	remoteGroups := make(map[int64][]int64)
	for i, target := range targets {
		response.Results[i].ID = target.rawID
		indexByID[target.id] = i
		client, err := h.svc.Get(ctx, target.id)
		if err != nil {
			response.Results[i].Error = bulkPublicError(err)
			continue
		}
		if client.NodeID == nil {
			localIDs = append(localIDs, target.id)
			continue
		}
		remoteGroups[*client.NodeID] = append(remoteGroups[*client.NodeID], target.id)
	}

	if len(localIDs) > 0 {
		results, err := localMutation(ctx, localIDs)
		h.logBulkError(op, err)
		applyBulkResults(response.Results, indexByID, results)
	}

	nodeIDs := make([]int64, 0, len(remoteGroups))
	for nodeID := range remoteGroups {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })
	for _, nodeID := range nodeIDs {
		ids := remoteGroups[nodeID]
		if h.nodes == nil {
			err := fmt.Errorf("%w: node mutations are not configured", svcnode.ErrValidation)
			applyBulkResults(response.Results, indexByID, bulkServiceFailures(ids, err))
			continue
		}
		results, err := remoteMutation(ctx, nodeID, ids)
		h.logBulkError(op, err)
		applyBulkResults(response.Results, indexByID, results)
	}
	return response
}

func (h *ClientHandler) remoteBulkDelete(ctx context.Context, nodeID int64, ids []int64) ([]svcclient.BulkResult, error) {
	return h.nodes.BulkDeleteClients(ctx, nodeID, ids)
}

func (h *ClientHandler) remoteBulkResetTraffic(ctx context.Context, nodeID int64, ids []int64) ([]svcclient.BulkResult, error) {
	return h.nodes.BulkResetClientTraffic(ctx, nodeID, ids)
}

func (h *ClientHandler) logBulkError(op string, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, svcnode.ErrNodeUnreachable) || errors.Is(err, svcnode.ErrRemote) {
		h.log.Warn(op, slog.String("error", bulkPublicError(err)))
		return
	}
	h.log.Error(op, slog.String("error", err.Error()))
}

func applyBulkResults(out []bulkClientResultDTO, indexByID map[int64]int, results []svcclient.BulkResult) {
	for _, result := range results {
		i, ok := indexByID[result.ID]
		if !ok {
			continue
		}
		out[i].OK = result.OK
		if !result.OK {
			out[i].Error = bulkPublicError(result.Err)
		}
	}
}

func bulkPublicError(err error) string {
	switch {
	case err == nil:
		return "internal error"
	case errors.Is(err, repo.ErrNotFound):
		return "not found"
	case errors.Is(err, svcnode.ErrNodeUnreachable):
		return "node unreachable"
	case errors.Is(err, svcnode.ErrRemote):
		return "remote node error"
	case errors.Is(err, svcclient.ErrValidation), errors.Is(err, svcnode.ErrValidation):
		return "invalid client"
	default:
		return "internal error"
	}
}

func bulkServiceFailures(ids []int64, err error) []svcclient.BulkResult {
	results := make([]svcclient.BulkResult, len(ids))
	for i, id := range ids {
		results[i] = svcclient.BulkResult{ID: id, Err: err}
	}
	return results
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
