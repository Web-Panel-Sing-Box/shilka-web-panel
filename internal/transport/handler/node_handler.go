package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"sing-box-web-panel/internal/domain"
	svcclient "sing-box-web-panel/internal/services/client"
	svcinbound "sing-box-web-panel/internal/services/inbound"
	svcnode "sing-box-web-panel/internal/services/node"
	"sing-box-web-panel/internal/services/singbox"
	"sing-box-web-panel/internal/services/sysstat"
	"sing-box-web-panel/internal/version"
)

type NodeHandler struct {
	svc        *svcnode.Service
	inbounds   *svcinbound.Service
	clients    *svcclient.Service
	sys        sysstat.Reader
	processMgr singbox.ProcessManager
	log        *slog.Logger
}

func NewNodeHandler(svc *svcnode.Service, inbounds *svcinbound.Service, clients *svcclient.Service, sys sysstat.Reader, pm singbox.ProcessManager, log *slog.Logger) *NodeHandler {
	return &NodeHandler{svc: svc, inbounds: inbounds, clients: clients, sys: sys, processMgr: pm, log: log}
}

func (h *NodeHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/nodes", h.List)
	mux.HandleFunc("POST /api/nodes", h.Create)
	mux.HandleFunc("GET /api/nodes/{id}", h.Get)
	mux.HandleFunc("PUT /api/nodes/{id}", h.Update)
	mux.HandleFunc("DELETE /api/nodes/{id}", h.Delete)
	mux.HandleFunc("POST /api/nodes/{id}/toggle", h.Toggle)
	mux.HandleFunc("POST /api/nodes/{id}/probe", h.Probe)
	mux.HandleFunc("POST /api/nodes/{id}/import", h.Sync)
	mux.HandleFunc("POST /api/nodes/{id}/sync", h.Sync)
	mux.HandleFunc("GET /api/nodes/{id}/status", h.Get)

	mux.HandleFunc("GET /api/node/v1/status", h.NodeStatus)
	mux.HandleFunc("GET /api/node/v1/snapshot", h.NodeSnapshot)
	mux.HandleFunc("POST /api/node/v1/inbounds", h.NodeCreateInbound)
	mux.HandleFunc("PUT /api/node/v1/inbounds/{id}", h.NodeUpdateInbound)
	mux.HandleFunc("DELETE /api/node/v1/inbounds/{id}", h.NodeDeleteInbound)
	mux.HandleFunc("POST /api/node/v1/inbounds/{id}/toggle", h.NodeToggleInbound)
	mux.HandleFunc("POST /api/node/v1/clients", h.NodeCreateClient)
	mux.HandleFunc("POST /api/node/v1/clients/bulk/delete", h.NodeBulkDeleteClients)
	mux.HandleFunc("POST /api/node/v1/clients/bulk/set-status", h.NodeBulkSetClientStatus)
	mux.HandleFunc("POST /api/node/v1/clients/bulk/reset-traffic", h.NodeBulkResetClientTraffic)
	mux.HandleFunc("PUT /api/node/v1/clients/{id}", h.NodeUpdateClient)
	mux.HandleFunc("DELETE /api/node/v1/clients/{id}", h.NodeDeleteClient)
	mux.HandleFunc("POST /api/node/v1/clients/{id}/reset-traffic", h.NodeResetClientTraffic)
	mux.HandleFunc("POST /api/node/v1/clients/{id}/status", h.NodeSetClientStatus)
	mux.HandleFunc("POST /api/node/v1/core/reload", h.NodeCoreReload)
}

type nodeDTO struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Remark              string  `json:"remark"`
	Scheme              string  `json:"scheme"`
	Address             string  `json:"address"`
	Port                int     `json:"port"`
	BasePath            string  `json:"basePath"`
	Enabled             bool    `json:"enabled"`
	AllowPrivateAddress bool    `json:"allowPrivateAddress"`
	SkipTLSVerify       bool    `json:"skipTlsVerify"`
	Status              string  `json:"status"`
	LastHeartbeatAt     string  `json:"lastHeartbeatAt,omitempty"`
	LatencyMS           int64   `json:"latencyMs"`
	PanelVersion        string  `json:"panelVersion"`
	CoreVersion         string  `json:"coreVersion"`
	CPUPct              float64 `json:"cpuPct"`
	RAMPct              float64 `json:"ramPct"`
	UptimeSeconds       int64   `json:"uptimeSeconds"`
	LastError           string  `json:"lastError,omitempty"`
	HasAPIToken         bool    `json:"hasApiToken"`
	CreatedAt           string  `json:"createdAt"`
	UpdatedAt           string  `json:"updatedAt"`
}

type nodeRequest struct {
	Name                string `json:"name"`
	Remark              string `json:"remark"`
	Scheme              string `json:"scheme"`
	Address             string `json:"address"`
	Port                int    `json:"port"`
	BasePath            string `json:"basePath"`
	APITokenSecret      string `json:"apiToken"`
	Enabled             *bool  `json:"enabled"`
	AllowPrivateAddress bool   `json:"allowPrivateAddress"`
	SkipTLSVerify       bool   `json:"skipTlsVerify"`
}

func toNodeDTO(n *domain.Node) nodeDTO {
	return nodeDTO{
		ID:                  strconv.FormatInt(n.ID, 10),
		Name:                n.Name,
		Remark:              n.Remark,
		Scheme:              n.Scheme,
		Address:             n.Address,
		Port:                n.Port,
		BasePath:            n.BasePath,
		Enabled:             n.Enabled,
		AllowPrivateAddress: n.AllowPrivateAddress,
		SkipTLSVerify:       n.SkipTLSVerify,
		Status:              string(n.Status),
		LastHeartbeatAt:     formatTimePtr(n.LastHeartbeatAt),
		LatencyMS:           n.LatencyMS,
		PanelVersion:        n.PanelVersion,
		CoreVersion:         n.CoreVersion,
		CPUPct:              n.CPUPct,
		RAMPct:              n.RAMPct,
		UptimeSeconds:       n.UptimeSeconds,
		LastError:           n.LastError,
		HasAPIToken:         n.APITokenSecret != "",
		CreatedAt:           n.CreatedAt.UTC().Format(timeRFC3339),
		UpdatedAt:           n.UpdatedAt.UTC().Format(timeRFC3339),
	}
}

func (req nodeRequest) toInput(defaultEnabled bool) svcnode.Input {
	enabled := defaultEnabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return svcnode.Input{
		Name:                req.Name,
		Remark:              req.Remark,
		Scheme:              req.Scheme,
		Address:             req.Address,
		Port:                req.Port,
		BasePath:            req.BasePath,
		APITokenSecret:      req.APITokenSecret,
		Enabled:             enabled,
		AllowPrivateAddress: req.AllowPrivateAddress,
		SkipTLSVerify:       req.SkipTLSVerify,
	}
}

func (h *NodeHandler) List(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.svc.List(r.Context())
	if err != nil {
		writeServiceError(w, h.log, "list nodes", err)
		return
	}
	out := make([]nodeDTO, 0, len(nodes))
	for i := range nodes {
		out = append(out, toNodeDTO(&nodes[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *NodeHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	n, err := h.svc.Create(r.Context(), req.toInput(true))
	if err != nil {
		writeServiceError(w, h.log, "create node", err)
		return
	}
	writeJSON(w, http.StatusCreated, toNodeDTO(n))
}

func (h *NodeHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	n, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "get node", err)
		return
	}
	writeJSON(w, http.StatusOK, toNodeDTO(n))
}

func (h *NodeHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.APITokenSecret) == "" {
		existing, err := h.svc.Get(r.Context(), id)
		if err != nil {
			writeServiceError(w, h.log, "get node", err)
			return
		}
		req.APITokenSecret = existing.APITokenSecret
	}
	n, err := h.svc.Update(r.Context(), id, req.toInput(true))
	if err != nil {
		writeServiceError(w, h.log, "update node", err)
		return
	}
	writeJSON(w, http.StatusOK, toNodeDTO(n))
}

func (h *NodeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		writeServiceError(w, h.log, "delete node", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *NodeHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	n, err := h.svc.Toggle(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "toggle node", err)
		return
	}
	writeJSON(w, http.StatusOK, toNodeDTO(n))
}

func (h *NodeHandler) Probe(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	n, err := h.svc.Probe(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "probe node", err)
		return
	}
	writeJSON(w, http.StatusOK, toNodeDTO(n))
}

func (h *NodeHandler) Sync(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	res, err := h.svc.Sync(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "sync node", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *NodeHandler) NodeStatus(w http.ResponseWriter, r *http.Request) {
	st := h.localStatus(r.Context())
	writeJSON(w, http.StatusOK, st)
}

func (h *NodeHandler) NodeSnapshot(w http.ResponseWriter, r *http.Request) {
	status := h.localStatus(r.Context())
	views, err := h.inbounds.List(r.Context())
	if err != nil {
		writeServiceError(w, h.log, "node snapshot inbounds", err)
		return
	}
	clients, err := h.clients.List(r.Context(), nil)
	if err != nil {
		writeServiceError(w, h.log, "node snapshot clients", err)
		return
	}
	out := svcnode.RemoteSnapshot{Status: status}
	for i := range views {
		ib := views[i].Inbound
		if ib.NodeID != nil {
			continue
		}
		settings := ib.Settings
		settings.RealityPrivateKey = ""
		out.Inbounds = append(out.Inbounds, svcnode.RemoteInbound{
			ID:           strconv.FormatInt(ib.ID, 10),
			Remark:       ib.Remark,
			Protocol:     ib.Protocol,
			Port:         ib.Port,
			Transmission: ib.Transmission,
			TLS:          ib.TLS,
			SNI:          ib.SNI,
			Dest:         ib.Dest,
			Enabled:      ib.Enabled,
			Settings:     settings,
			UpdatedAt:    ib.UpdatedAt.UTC().Format(timeRFC3339),
		})
	}
	for i := range clients {
		c := clients[i]
		if c.NodeID != nil {
			continue
		}
		out.Clients = append(out.Clients, svcnode.RemoteClient{
			ID:                 strconv.FormatInt(c.ID, 10),
			InboundID:          strconv.FormatInt(c.InboundID, 10),
			Name:               c.Name,
			UUID:               c.UUID,
			Password:           c.Password,
			UsedUp:             c.UsedUp,
			UsedDown:           c.UsedDown,
			TotalQuota:         c.TotalQuota,
			Expiry:             formatTimePtr(c.Expiry),
			Status:             c.Status,
			SubToken:           c.SubToken,
			StartAfterFirstUse: c.StartAfterFirstUse,
			Enabled:            c.Enabled,
			FirstUsedAt:        formatTimePtr(c.FirstUsedAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *NodeHandler) NodeCoreReload(w http.ResponseWriter, r *http.Request) {
	if err := h.processMgr.Reload(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "reload ok"})
}

func (h *NodeHandler) NodeCreateInbound(w http.ResponseWriter, r *http.Request) {
	var req inboundRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ib, err := h.inbounds.Create(r.Context(), req.toInput())
	if err != nil {
		writeServiceError(w, h.log, "node create inbound", err)
		return
	}
	writeJSON(w, http.StatusCreated, remoteInboundFromDomain(ib))
}

func (h *NodeHandler) NodeUpdateInbound(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	var req inboundRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ib, err := h.inbounds.Update(r.Context(), id, req.toInput())
	if err != nil {
		writeServiceError(w, h.log, "node update inbound", err)
		return
	}
	writeJSON(w, http.StatusOK, remoteInboundFromDomain(ib))
}

func (h *NodeHandler) NodeDeleteInbound(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	if err := h.inbounds.Delete(r.Context(), id); err != nil {
		writeServiceError(w, h.log, "node delete inbound", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *NodeHandler) NodeToggleInbound(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	ib, err := h.inbounds.Toggle(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "node toggle inbound", err)
		return
	}
	writeJSON(w, http.StatusOK, remoteInboundFromDomain(ib))
}

func (h *NodeHandler) NodeCreateClient(w http.ResponseWriter, r *http.Request) {
	var req createClientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	inboundID, err := strconv.ParseInt(req.InboundID, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid inboundId"})
		return
	}
	expiry, err := parseTimePtr(req.Expiry)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid expiry"})
		return
	}
	c, err := h.clients.Create(r.Context(), svcclient.CreateInput{
		Name:               req.Name,
		InboundID:          inboundID,
		TotalQuota:         req.TotalQuota,
		Expiry:             expiry,
		StartAfterFirstUse: req.StartAfterFirstUse,
	})
	if err != nil {
		writeServiceError(w, h.log, "node create client", err)
		return
	}
	writeJSON(w, http.StatusCreated, remoteClientFromDomain(c))
}

func (h *NodeHandler) NodeUpdateClient(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	var req updateClientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := svcclient.UpdateInput{
		Name:               req.Name,
		TotalQuota:         req.TotalQuota,
		StartAfterFirstUse: req.StartAfterFirstUse,
	}
	if req.InboundID != nil {
		inboundID, err := strconv.ParseInt(*req.InboundID, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid inboundId"})
			return
		}
		in.InboundID = &inboundID
	}
	if req.Expiry != nil {
		expiry, err := parseTimePtr(*req.Expiry)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid expiry"})
			return
		}
		in.Expiry = expiry
	}
	if req.Status != nil {
		status := domain.ClientStatus(*req.Status)
		in.Status = &status
	}
	c, err := h.clients.Update(r.Context(), id, in)
	if err != nil {
		writeServiceError(w, h.log, "node update client", err)
		return
	}
	writeJSON(w, http.StatusOK, remoteClientFromDomain(c))
}

func (h *NodeHandler) NodeDeleteClient(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	if err := h.clients.Delete(r.Context(), id); err != nil {
		writeServiceError(w, h.log, "node delete client", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *NodeHandler) NodeResetClientTraffic(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	c, err := h.clients.ResetTraffic(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "node reset client traffic", err)
		return
	}
	writeJSON(w, http.StatusOK, remoteClientFromDomain(c))
}

func (h *NodeHandler) NodeSetClientStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	var req setStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	c, err := h.clients.SetStatus(r.Context(), id, domain.ClientStatus(req.Status))
	if err != nil {
		writeServiceError(w, h.log, "node set client status", err)
		return
	}
	writeJSON(w, http.StatusOK, remoteClientFromDomain(c))
}

// NodeBulkDeleteClients godoc
//
//	@Summary	Delete multiple local clients through the node API
//	@Tags		node
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		bulkClientRequest	true	"Client IDs"
//	@Success	200		{object}	bulkClientResponse
//	@Router		/node/v1/clients/bulk/delete [post]
func (h *NodeHandler) NodeBulkDeleteClients(w http.ResponseWriter, r *http.Request) {
	targets, ok := decodeBulkClientTargets(w, r)
	if !ok {
		return
	}
	ids := bulkTargetIDs(targets)
	results, err := h.clients.BulkDelete(r.Context(), ids)
	h.writeNodeBulkResults(w, "node bulk delete clients", targets, results, err)
}

// NodeBulkResetClientTraffic godoc
//
//	@Summary	Reset multiple local client counters through the node API
//	@Tags		node
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		bulkClientRequest	true	"Client IDs"
//	@Success	200		{object}	bulkClientResponse
//	@Router		/node/v1/clients/bulk/reset-traffic [post]
func (h *NodeHandler) NodeBulkResetClientTraffic(w http.ResponseWriter, r *http.Request) {
	targets, ok := decodeBulkClientTargets(w, r)
	if !ok {
		return
	}
	ids := bulkTargetIDs(targets)
	results, err := h.clients.BulkResetTraffic(r.Context(), ids)
	h.writeNodeBulkResults(w, "node bulk reset client traffic", targets, results, err)
}

// NodeBulkSetClientStatus godoc
//
//	@Summary	Set multiple local client statuses through the node API
//	@Tags		node
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		bulkClientRequest	true	"Client IDs and status"
//	@Success	200		{object}	bulkClientResponse
//	@Router		/node/v1/clients/bulk/set-status [post]
func (h *NodeHandler) NodeBulkSetClientStatus(w http.ResponseWriter, r *http.Request) {
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
	ids := bulkTargetIDs(targets)
	results, err := h.clients.BulkSetStatus(r.Context(), ids, status)
	h.writeNodeBulkResults(w, "node bulk set client status", targets, results, err)
}

func bulkTargetIDs(targets []bulkClientTarget) []int64 {
	ids := make([]int64, len(targets))
	for i, target := range targets {
		ids[i] = target.id
	}
	return ids
}

func (h *NodeHandler) writeNodeBulkResults(
	w http.ResponseWriter,
	op string,
	targets []bulkClientTarget,
	results []svcclient.BulkResult,
	err error,
) {
	if err != nil {
		h.log.Error(op, slog.String("error", err.Error()))
	}
	response := bulkClientResponse{Results: make([]bulkClientResultDTO, len(targets))}
	indexByID := make(map[int64]int, len(targets))
	for i, target := range targets {
		response.Results[i].ID = target.rawID
		indexByID[target.id] = i
	}
	applyBulkResults(response.Results, indexByID, results)
	writeJSON(w, http.StatusOK, response)
}

func (h *NodeHandler) localStatus(ctx context.Context) svcnode.RemoteStatus {
	var out svcnode.RemoteStatus
	if st, err := h.processMgr.Status(ctx); err == nil {
		out.CoreVersion = st.Version
		if st.Running {
			out.CoreStatus = "running"
		} else {
			out.CoreStatus = "stopped"
		}
		out.UptimeSeconds = int64(st.Uptime.Seconds())
	}
	if m, err := h.sys.Read(); err == nil {
		out.CPUPct = m.CPU * 100
		out.RAMPct = m.RAM * 100
		if out.UptimeSeconds == 0 {
			out.UptimeSeconds = m.UptimeSeconds
		}
	}
	if views, err := h.inbounds.List(ctx); err == nil {
		for i := range views {
			if views[i].Inbound.NodeID == nil {
				out.InboundCount++
			}
		}
	}
	if clients, err := h.clients.List(ctx, nil); err == nil {
		for i := range clients {
			if clients[i].NodeID == nil {
				out.ClientCount++
				if clients[i].QuotaExceeded() || clients[i].Status == domain.ClientStatusExpired {
					out.DepletedCount++
				}
			}
		}
	}
	out.PanelVersion = version.Panel()
	return out
}

func remoteInboundFromDomain(ib *domain.Inbound) svcnode.RemoteInbound {
	settings := ib.Settings
	settings.RealityPrivateKey = ""
	return svcnode.RemoteInbound{
		ID:           strconv.FormatInt(ib.ID, 10),
		Remark:       ib.Remark,
		Protocol:     ib.Protocol,
		Port:         ib.Port,
		Transmission: ib.Transmission,
		TLS:          ib.TLS,
		SNI:          ib.SNI,
		Dest:         ib.Dest,
		Enabled:      ib.Enabled,
		Settings:     settings,
		UpdatedAt:    ib.UpdatedAt.UTC().Format(timeRFC3339),
	}
}

func remoteClientFromDomain(c *domain.Client) svcnode.RemoteClient {
	return svcnode.RemoteClient{
		ID:                 strconv.FormatInt(c.ID, 10),
		InboundID:          strconv.FormatInt(c.InboundID, 10),
		Name:               c.Name,
		UUID:               c.UUID,
		Password:           c.Password,
		UsedUp:             c.UsedUp,
		UsedDown:           c.UsedDown,
		TotalQuota:         c.TotalQuota,
		Expiry:             formatTimePtr(c.Expiry),
		Status:             c.Status,
		SubToken:           c.SubToken,
		StartAfterFirstUse: c.StartAfterFirstUse,
		Enabled:            c.Enabled,
		FirstUsedAt:        formatTimePtr(c.FirstUsedAt),
	}
}
