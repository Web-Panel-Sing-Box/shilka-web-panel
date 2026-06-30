package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"sing-box-web-panel/internal/domain"
	svcinbound "sing-box-web-panel/internal/services/inbound"
	svcnode "sing-box-web-panel/internal/services/node"
)

type InboundHandler struct {
	svc   *svcinbound.Service
	nodes *svcnode.Service
	log   *slog.Logger
}

func NewInboundHandler(svc *svcinbound.Service, log *slog.Logger, nodes ...*svcnode.Service) *InboundHandler {
	h := &InboundHandler{svc: svc, log: log}
	if len(nodes) > 0 {
		h.nodes = nodes[0]
	}
	return h
}

func (h *InboundHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/inbounds", h.List)
	mux.HandleFunc("POST /api/inbounds", h.Create)
	mux.HandleFunc("GET /api/inbounds/{id}", h.Get)
	mux.HandleFunc("PUT /api/inbounds/{id}", h.Update)
	mux.HandleFunc("DELETE /api/inbounds/{id}", h.Delete)
	mux.HandleFunc("POST /api/inbounds/{id}/toggle", h.Toggle)
	mux.HandleFunc("POST /api/inbounds/{id}/clone", h.Clone)
	mux.HandleFunc("POST /api/nodes/{id}/inbounds", h.CreateOnNode)
}

type inboundSettingsDTO struct {
	PublicKey       string `json:"publicKey,omitempty"`
	ShortID         string `json:"shortId,omitempty"`
	Flow            string `json:"flow,omitempty"`
	WSPath          string `json:"wsPath,omitempty"`
	GRPCServiceName string `json:"grpcServiceName,omitempty"`
	HTTPUpgradePath string `json:"httpUpgradePath,omitempty"`
	HTTPUpgradeHost string `json:"httpUpgradeHost,omitempty"`
	// VLESS multiplex.
	MultiplexEnabled bool `json:"multiplexEnabled,omitempty"`
	// Hysteria2.
	Hy2UpMbps                int    `json:"hy2UpMbps,omitempty"`
	Hy2DownMbps              int    `json:"hy2DownMbps,omitempty"`
	Hy2IgnoreClientBandwidth bool   `json:"hy2IgnoreClientBandwidth,omitempty"`
	Hy2ObfsPassword          string `json:"hy2ObfsPassword,omitempty"`
	Hy2ObfsMinPacketSize     int    `json:"hy2ObfsMinPacketSize,omitempty"`
	Hy2ObfsMaxPacketSize     int    `json:"hy2ObfsMaxPacketSize,omitempty"`
	Hy2Masquerade            string `json:"hy2Masquerade,omitempty"`
	Hy2Network               string `json:"hy2Network,omitempty"`
	Hy2BrutalDebug           bool   `json:"hy2BrutalDebug,omitempty"`
	Hy2BbrProfile            string `json:"hy2BbrProfile,omitempty"`
	// Naive.
	NaiveNetwork            string `json:"naiveNetwork,omitempty"`
	NaiveQuicCongestionCtrl string `json:"naiveQuicCongestionCtrl,omitempty"`
	// Client subscription TLS verification.
	AllowInsecure *bool `json:"allowInsecure,omitempty"`
	// TLS certificate source (tls mode). Empty cert/acme means the panel falls
	// back to a self-signed cert (SIN-52).
	ACMEDomain string `json:"acmeDomain,omitempty"`
	ACMEEmail  string `json:"acmeEmail,omitempty"`
	CertPath   string `json:"certPath,omitempty"`
	KeyPath    string `json:"keyPath,omitempty"`
}

type inboundDTO struct {
	ID           string              `json:"id"`
	NodeID       string              `json:"nodeId,omitempty"`
	RemoteID     string              `json:"remoteId,omitempty"`
	Remark       string              `json:"remark"`
	Protocol     string              `json:"protocol"`
	Port         int                 `json:"port"`
	Transmission string              `json:"transmission"`
	TLS          string              `json:"tls"`
	SNI          string              `json:"sni,omitempty"`
	Dest         string              `json:"dest,omitempty"`
	Enabled      bool                `json:"enabled"`
	ClientCount  int                 `json:"clientCount"`
	CreatedAt    string              `json:"createdAt"`
	UpdatedAt    string              `json:"updatedAt"`
	Settings     *inboundSettingsDTO `json:"settings,omitempty"`
}

func toInboundDTO(ib *domain.Inbound, clientCount int) inboundDTO {
	dto := inboundDTO{
		ID:           strconv.FormatInt(ib.ID, 10),
		RemoteID:     ib.RemoteID,
		Remark:       ib.Remark,
		Protocol:     string(ib.Protocol),
		Port:         ib.Port,
		Transmission: string(ib.Transmission),
		TLS:          string(ib.TLS),
		SNI:          ib.SNI,
		Dest:         ib.Dest,
		Enabled:      ib.Enabled,
		ClientCount:  clientCount,
		CreatedAt:    ib.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    ib.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if ib.NodeID != nil {
		dto.NodeID = strconv.FormatInt(*ib.NodeID, 10)
	}
	// Surface only non-secret settings; never expose the Reality private key.
	allowInsecure := ib.EffectiveAllowInsecure()
	s := inboundSettingsDTO{
		PublicKey:                ib.Settings.RealityPublicKey,
		ShortID:                  ib.Settings.RealityShortID,
		Flow:                     ib.Settings.Flow,
		WSPath:                   ib.Settings.WSPath,
		GRPCServiceName:          ib.Settings.GRPCServiceName,
		HTTPUpgradePath:          ib.Settings.HTTPUpgradePath,
		HTTPUpgradeHost:          ib.Settings.HTTPUpgradeHost,
		MultiplexEnabled:         ib.Settings.MultiplexEnabled,
		Hy2UpMbps:                ib.Settings.Hy2UpMbps,
		Hy2DownMbps:              ib.Settings.Hy2DownMbps,
		Hy2IgnoreClientBandwidth: ib.Settings.Hy2IgnoreClientBandwidth,
		Hy2ObfsMinPacketSize:     ib.Settings.Hy2ObfsMinPacketSize,
		Hy2ObfsMaxPacketSize:     ib.Settings.Hy2ObfsMaxPacketSize,
		Hy2Masquerade:            ib.Settings.Hy2Masquerade,
		Hy2Network:               ib.Settings.Hy2Network,
		Hy2BrutalDebug:           ib.Settings.Hy2BrutalDebug,
		Hy2BbrProfile:            ib.Settings.Hy2BbrProfile,
		NaiveNetwork:             ib.Settings.NaiveNetwork,
		NaiveQuicCongestionCtrl:  ib.Settings.NaiveQuicCongestionCtrl,
		AllowInsecure:            &allowInsecure,
		ACMEDomain:               ib.Settings.ACMEDomain,
		ACMEEmail:                ib.Settings.ACMEEmail,
		CertPath:                 ib.Settings.CertPath,
		KeyPath:                  ib.Settings.KeyPath,
	}
	if s != (inboundSettingsDTO{}) {
		dto.Settings = &s
	}
	return dto
}

type inboundRequest struct {
	NodeID        string `json:"nodeId,omitempty"`
	Remark        string `json:"remark"`
	Protocol      string `json:"protocol"`
	Port          int    `json:"port"`
	Transmission  string `json:"transmission"`
	TLS           string `json:"tls"`
	SNI           string `json:"sni"`
	Dest          string `json:"dest"`
	ACMEDomain    string `json:"acmeDomain,omitempty"`
	ACMEEmail     string `json:"acmeEmail,omitempty"`
	CertPath      string `json:"certPath,omitempty"`
	KeyPath       string `json:"keyPath,omitempty"`
	AllowInsecure *bool  `json:"allowInsecure,omitempty"`
	// VLESS transport settings.
	WSPath          string `json:"wsPath,omitempty"`
	GRPCServiceName string `json:"grpcServiceName,omitempty"`
	HTTPUpgradePath string `json:"httpUpgradePath,omitempty"`
	HTTPUpgradeHost string `json:"httpUpgradeHost,omitempty"`
	// VLESS multiplex.
	MultiplexEnabled bool `json:"multiplexEnabled,omitempty"`
	// Hysteria2.
	Hy2UpMbps                int    `json:"hy2UpMbps,omitempty"`
	Hy2DownMbps              int    `json:"hy2DownMbps,omitempty"`
	Hy2IgnoreClientBandwidth bool   `json:"hy2IgnoreClientBandwidth,omitempty"`
	Hy2ObfsPassword          string `json:"hy2ObfsPassword,omitempty"`
	Hy2ObfsMinPacketSize     int    `json:"hy2ObfsMinPacketSize,omitempty"`
	Hy2ObfsMaxPacketSize     int    `json:"hy2ObfsMaxPacketSize,omitempty"`
	Hy2Masquerade            string `json:"hy2Masquerade,omitempty"`
	Hy2Network               string `json:"hy2Network,omitempty"`
	Hy2BrutalDebug           bool   `json:"hy2BrutalDebug,omitempty"`
	Hy2BbrProfile            string `json:"hy2BbrProfile,omitempty"`
	// Naive.
	NaiveNetwork            string `json:"naiveNetwork,omitempty"`
	NaiveQuicCongestionCtrl string `json:"naiveQuicCongestionCtrl,omitempty"`
}

func (req inboundRequest) toInput() svcinbound.Input {
	return svcinbound.Input{
		Remark:                   req.Remark,
		Protocol:                 domain.Protocol(req.Protocol),
		Port:                     req.Port,
		Transmission:             domain.Transmission(req.Transmission),
		TLS:                      domain.TLSMode(req.TLS),
		SNI:                      req.SNI,
		Dest:                     req.Dest,
		ACMEDomain:               req.ACMEDomain,
		ACMEEmail:                req.ACMEEmail,
		CertPath:                 req.CertPath,
		KeyPath:                  req.KeyPath,
		AllowInsecure:            req.AllowInsecure,
		WSPath:                   req.WSPath,
		GRPCServiceName:          req.GRPCServiceName,
		HTTPUpgradePath:          req.HTTPUpgradePath,
		HTTPUpgradeHost:          req.HTTPUpgradeHost,
		MultiplexEnabled:         req.MultiplexEnabled,
		Hy2UpMbps:                req.Hy2UpMbps,
		Hy2DownMbps:              req.Hy2DownMbps,
		Hy2IgnoreClientBandwidth: req.Hy2IgnoreClientBandwidth,
		Hy2ObfsPassword:          req.Hy2ObfsPassword,
		Hy2ObfsMinPacketSize:     req.Hy2ObfsMinPacketSize,
		Hy2ObfsMaxPacketSize:     req.Hy2ObfsMaxPacketSize,
		Hy2Masquerade:            req.Hy2Masquerade,
		Hy2Network:               req.Hy2Network,
		Hy2BrutalDebug:           req.Hy2BrutalDebug,
		Hy2BbrProfile:            req.Hy2BbrProfile,
		NaiveNetwork:             req.NaiveNetwork,
		NaiveQuicCongestionCtrl:  req.NaiveQuicCongestionCtrl,
	}
}

// List godoc
//
//	@Summary	List inbounds
//	@Tags		inbounds
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{array}	inboundDTO
//	@Router		/inbounds [get]
func (h *InboundHandler) List(w http.ResponseWriter, r *http.Request) {
	views, err := h.svc.List(r.Context())
	if err != nil {
		writeServiceError(w, h.log, "list inbounds", err)
		return
	}
	out := make([]inboundDTO, 0, len(views))
	for i := range views {
		out = append(out, toInboundDTO(&views[i].Inbound, views[i].ClientCount))
	}
	writeJSON(w, http.StatusOK, out)
}

// Create godoc
//
//	@Summary	Create an inbound
//	@Tags		inbounds
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		inboundRequest	true	"Inbound"
//	@Success	201		{object}	inboundDTO
//	@Router		/inbounds [post]
func (h *InboundHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req inboundRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.NodeID != "" {
		nodeID, ok := parsePositiveID(w, req.NodeID, "invalid nodeId")
		if !ok {
			return
		}
		h.createRemote(w, r, nodeID, req)
		return
	}
	ib, err := h.svc.Create(r.Context(), req.toInput())
	if err != nil {
		writeServiceError(w, h.log, "create inbound", err)
		return
	}
	writeJSON(w, http.StatusCreated, toInboundDTO(ib, 0))
}

func (h *InboundHandler) CreateOnNode(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid nodeId"})
		return
	}
	var req inboundRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.NodeID != "" && !matchesNodeID(w, req.NodeID, nodeID) {
		return
	}
	h.createRemote(w, r, nodeID, req)
}

func (h *InboundHandler) createRemote(w http.ResponseWriter, r *http.Request, nodeID int64, req inboundRequest) {
	if h.nodes == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node mutations are not configured"})
		return
	}
	ib, err := h.nodes.CreateInbound(r.Context(), nodeID, req.toInput())
	if err != nil {
		writeServiceError(w, h.log, "create remote inbound", err)
		return
	}
	writeJSON(w, http.StatusCreated, toInboundDTO(ib, 0))
}

// Get godoc
//
//	@Summary	Get an inbound
//	@Tags		inbounds
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		int	true	"Inbound ID"
//	@Success	200	{object}	inboundDTO
//	@Router		/inbounds/{id} [get]
func (h *InboundHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	ib, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "get inbound", err)
		return
	}
	clientCount, err := h.svc.ClientCount(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "count inbound clients", err)
		return
	}
	writeJSON(w, http.StatusOK, toInboundDTO(ib, clientCount))
}

// Update godoc
//
//	@Summary	Update an inbound
//	@Tags		inbounds
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		int				true	"Inbound ID"
//	@Param		request	body		inboundRequest	true	"Inbound"
//	@Success	200		{object}	inboundDTO
//	@Router		/inbounds/{id} [put]
func (h *InboundHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	var req inboundRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	existing, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "get inbound", err)
		return
	}
	clientCount, err := h.svc.ClientCount(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "count inbound clients", err)
		return
	}
	if existing.NodeID != nil {
		if h.nodes == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node mutations are not configured"})
			return
		}
		if req.NodeID != "" && !matchesNodeID(w, req.NodeID, *existing.NodeID) {
			return
		}
		ib, err := h.nodes.UpdateInbound(r.Context(), id, req.toInput())
		if err != nil {
			writeServiceError(w, h.log, "update remote inbound", err)
			return
		}
		writeJSON(w, http.StatusOK, toInboundDTO(ib, clientCount))
		return
	}
	if req.NodeID != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot move inbound between nodes"})
		return
	}
	ib, err := h.svc.Update(r.Context(), id, req.toInput())
	if err != nil {
		writeServiceError(w, h.log, "update inbound", err)
		return
	}
	writeJSON(w, http.StatusOK, toInboundDTO(ib, clientCount))
}

// Delete godoc
//
//	@Summary	Delete an inbound
//	@Tags		inbounds
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path	int	true	"Inbound ID"
//	@Success	200	{object}	map[string]string
//	@Router		/inbounds/{id} [delete]
func (h *InboundHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	existing, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "get inbound", err)
		return
	}
	if existing.NodeID != nil {
		if h.nodes == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node mutations are not configured"})
			return
		}
		if err := h.nodes.DeleteInbound(r.Context(), id); err != nil {
			writeServiceError(w, h.log, "delete remote inbound", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		writeServiceError(w, h.log, "delete inbound", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// Toggle godoc
//
//	@Summary	Enable/disable an inbound
//	@Tags		inbounds
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		int	true	"Inbound ID"
//	@Success	200	{object}	inboundDTO
//	@Router		/inbounds/{id}/toggle [post]
func (h *InboundHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	existing, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "get inbound", err)
		return
	}
	clientCount, err := h.svc.ClientCount(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "count inbound clients", err)
		return
	}
	if existing.NodeID != nil {
		if h.nodes == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node mutations are not configured"})
			return
		}
		ib, err := h.nodes.ToggleInbound(r.Context(), id)
		if err != nil {
			writeServiceError(w, h.log, "toggle remote inbound", err)
			return
		}
		writeJSON(w, http.StatusOK, toInboundDTO(ib, clientCount))
		return
	}
	ib, err := h.svc.Toggle(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "toggle inbound", err)
		return
	}
	writeJSON(w, http.StatusOK, toInboundDTO(ib, clientCount))
}

// Clone godoc
//
//	@Summary	Clone an inbound
//	@Tags		inbounds
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		int	true	"Inbound ID"
//	@Success	201	{object}	inboundDTO
//	@Router		/inbounds/{id}/clone [post]
func (h *InboundHandler) Clone(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	ib, err := h.svc.Clone(r.Context(), id)
	if err != nil {
		writeServiceError(w, h.log, "clone inbound", err)
		return
	}
	writeJSON(w, http.StatusCreated, toInboundDTO(ib, 0))
}
