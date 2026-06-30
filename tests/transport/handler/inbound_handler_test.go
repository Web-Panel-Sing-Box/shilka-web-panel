package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
	svcinbound "sing-box-web-panel/internal/services/inbound"
	svcnode "sing-box-web-panel/internal/services/node"
	"sing-box-web-panel/internal/transport/handler"
)

type inboundFakeRepo struct {
	items  map[int64]*domain.Inbound
	nextID int64
}

func newInboundFakeRepo() *inboundFakeRepo {
	return &inboundFakeRepo{items: map[int64]*domain.Inbound{}}
}

func (r *inboundFakeRepo) Create(_ context.Context, ib *domain.Inbound) error {
	r.nextID++
	ib.ID = r.nextID
	cp := *ib
	r.items[ib.ID] = &cp
	return nil
}

func (r *inboundFakeRepo) GetByID(_ context.Context, id int64) (*domain.Inbound, error) {
	if ib, ok := r.items[id]; ok {
		cp := *ib
		return &cp, nil
	}
	return nil, repo.ErrNotFound
}

func (r *inboundFakeRepo) GetByRemote(_ context.Context, nodeID int64, remoteID string) (*domain.Inbound, error) {
	for _, ib := range r.items {
		if ib.NodeID != nil && *ib.NodeID == nodeID && ib.RemoteID == remoteID {
			cp := *ib
			return &cp, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (r *inboundFakeRepo) List(context.Context) ([]domain.Inbound, error) { return nil, nil }

func (r *inboundFakeRepo) Update(_ context.Context, ib *domain.Inbound) error {
	cp := *ib
	r.items[ib.ID] = &cp
	return nil
}

func (r *inboundFakeRepo) SetEnabled(_ context.Context, id int64, enabled bool) error {
	if ib, ok := r.items[id]; ok {
		ib.Enabled = enabled
		return nil
	}
	return repo.ErrNotFound
}

func (r *inboundFakeRepo) Delete(_ context.Context, id int64) error {
	delete(r.items, id)
	return nil
}

func (r *inboundFakeRepo) UpsertRemote(_ context.Context, nodeID int64, remoteID string, ib *domain.Inbound) (int64, error) {
	for id, existing := range r.items {
		if existing.NodeID != nil && *existing.NodeID == nodeID && existing.RemoteID == remoteID {
			cp := *ib
			cp.ID = id
			cp.NodeID = ptrInt64(nodeID)
			cp.RemoteID = remoteID
			r.items[id] = &cp
			return id, nil
		}
	}
	r.nextID++
	cp := *ib
	cp.ID = r.nextID
	cp.NodeID = ptrInt64(nodeID)
	cp.RemoteID = remoteID
	r.items[cp.ID] = &cp
	return cp.ID, nil
}

type inboundFakeCounter struct {
	counts map[int64]int
}

func (c inboundFakeCounter) CountByInbound(context.Context) (map[int64]int, error) {
	if c.counts != nil {
		return c.counts, nil
	}
	return map[int64]int{}, nil
}

func TestInboundHandlerGetReturnsClientCount(t *testing.T) {
	repo := newInboundFakeRepo()
	ib := seedInbound(t, repo)
	mux := testInboundMuxWithCounter(repo, inboundFakeCounter{counts: map[int64]int{ib.ID: 3}})

	req := httptest.NewRequest(http.MethodGet, "/api/inbounds/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got := responseInbound(t, rec.Body.Bytes()).ClientCount; got != 3 {
		t.Fatalf("clientCount = %d, want 3", got)
	}
}

func TestInboundHandlerUpdateReturnsClientCount(t *testing.T) {
	repo := newInboundFakeRepo()
	ib := seedInbound(t, repo)
	mux := testInboundMuxWithCounter(repo, inboundFakeCounter{counts: map[int64]int{ib.ID: 4}})

	body, _ := json.Marshal(map[string]any{
		"remark":       "edge-updated",
		"protocol":     "vless",
		"port":         9443,
		"transmission": "tcp",
		"tls":          "none",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/inbounds/1", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	resp := responseInbound(t, rec.Body.Bytes())
	if resp.ClientCount != 4 {
		t.Fatalf("clientCount = %d, want 4", resp.ClientCount)
	}
	if resp.Remark != "edge-updated" {
		t.Fatalf("remark = %q, want edge-updated", resp.Remark)
	}
}

func TestInboundHandlerToggleReturnsClientCount(t *testing.T) {
	repo := newInboundFakeRepo()
	ib := seedInbound(t, repo)
	mux := testInboundMuxWithCounter(repo, inboundFakeCounter{counts: map[int64]int{ib.ID: 5}})

	req := httptest.NewRequest(http.MethodPost, "/api/inbounds/1/toggle", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	resp := responseInbound(t, rec.Body.Bytes())
	if resp.ClientCount != 5 {
		t.Fatalf("clientCount = %d, want 5", resp.ClientCount)
	}
	if resp.Enabled {
		t.Fatal("enabled = true, want false")
	}
}

func TestInboundHandlerCreateAllowInsecureExplicitFalse(t *testing.T) {
	repo := newInboundFakeRepo()
	mux := testInboundMux(repo)

	body, _ := json.Marshal(map[string]any{
		"remark":        "hy2",
		"protocol":      "hysteria2",
		"port":          8443,
		"tls":           "tls",
		"allowInsecure": false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if repo.items[1].Settings.AllowInsecure == nil || *repo.items[1].Settings.AllowInsecure {
		t.Fatalf("stored allow insecure = %#v, want explicit false", repo.items[1].Settings.AllowInsecure)
	}
	settings := responseSettings(t, rec.Body.Bytes())
	if got := settings["allowInsecure"]; got != false {
		t.Fatalf("response settings.allowInsecure = %#v, want false", got)
	}
}

func TestInboundHandlerCreateAllowInsecureAutoResponse(t *testing.T) {
	mux := testInboundMux(newInboundFakeRepo())

	body, _ := json.Marshal(map[string]any{
		"remark":   "hy2",
		"protocol": "hysteria2",
		"port":     8443,
		"tls":      "tls",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	settings := responseSettings(t, rec.Body.Bytes())
	if got := settings["allowInsecure"]; got != true {
		t.Fatalf("response settings.allowInsecure = %#v, want true", got)
	}
}

func TestInboundHandlerCreateHTTPUpgradeRoundTrip(t *testing.T) {
	repo := newInboundFakeRepo()
	mux := testInboundMux(repo)
	body, _ := json.Marshal(map[string]any{
		"remark":          "upgrade",
		"protocol":        "vless",
		"port":            8080,
		"transmission":    "httpupgrade",
		"tls":             "none",
		"httpUpgradePath": "/upgrade",
		"httpUpgradeHost": "edge.example.com",
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/inbounds", bytes.NewReader(body)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if repo.items[1].Transmission != domain.TransmissionHTTPUpgrade {
		t.Fatalf("stored transmission = %q", repo.items[1].Transmission)
	}
	settings := responseSettings(t, rec.Body.Bytes())
	if settings["httpUpgradePath"] != "/upgrade" || settings["httpUpgradeHost"] != "edge.example.com" {
		t.Fatalf("response settings = %#v", settings)
	}
}

func TestInboundHandlerCreateOnNodeCachesRemoteInbound(t *testing.T) {
	repo := newInboundFakeRepo()
	mux := testInboundRemoteMux(repo)
	body, _ := json.Marshal(map[string]any{
		"remark":   "remote-hy2",
		"protocol": "hysteria2",
		"port":     8443,
		"tls":      "tls",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/1/inbounds", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	got := repo.items[1]
	if got == nil || got.NodeID == nil || *got.NodeID != 1 || got.RemoteID != "99" || got.Remark != "remote-hy2" {
		t.Fatalf("cached inbound = %+v", got)
	}
}

func TestInboundHandlerCreateOnNodeRejectsBodyNodeMismatch(t *testing.T) {
	mux := testInboundRemoteMux(newInboundFakeRepo())
	body, _ := json.Marshal(map[string]any{
		"nodeId":   "2",
		"remark":   "remote-hy2",
		"protocol": "hysteria2",
		"port":     8443,
		"tls":      "tls",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/1/inbounds", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

func testInboundMux(repo *inboundFakeRepo) *http.ServeMux {
	return testInboundMuxWithCounter(repo, inboundFakeCounter{})
}

func testInboundMuxWithCounter(repo *inboundFakeRepo, counter inboundFakeCounter) *http.ServeMux {
	svc := svcinbound.NewService(repo, counter, nil)
	h := handler.NewInboundHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func testInboundRemoteMux(repo *inboundFakeRepo) *http.ServeMux {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := svcinbound.NewService(repo, inboundFakeCounter{}, nil)
	nodeSvc := svcnode.NewService(
		&handlerNodeRepo{nodes: map[int64]*domain.Node{1: {ID: 1, Enabled: true, APITokenSecret: "secret"}}},
		repo,
		handlerClientCache{},
		handlerRemoteClienter{},
	)
	h := handler.NewInboundHandler(svc, log, nodeSvc)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func responseSettings(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	settings, ok := resp["settings"].(map[string]any)
	if !ok {
		t.Fatalf("settings missing or invalid in response: %s", body)
	}
	return settings
}

type inboundResponse struct {
	Remark      string `json:"remark"`
	Enabled     bool   `json:"enabled"`
	ClientCount int    `json:"clientCount"`
}

func responseInbound(t *testing.T, body []byte) inboundResponse {
	t.Helper()
	var resp inboundResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal inbound response: %v", err)
	}
	return resp
}

func seedInbound(t *testing.T, repo *inboundFakeRepo) *domain.Inbound {
	t.Helper()
	now := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	ib := &domain.Inbound{
		Remark:       "edge",
		Protocol:     domain.ProtocolVLESS,
		Port:         443,
		Transmission: domain.TransmissionTCP,
		TLS:          domain.TLSModeNone,
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.Create(context.Background(), ib); err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	return ib
}

type handlerNodeRepo struct {
	nodes map[int64]*domain.Node
}

func (r *handlerNodeRepo) Create(context.Context, *domain.Node) error { return nil }
func (r *handlerNodeRepo) GetByID(_ context.Context, id int64) (*domain.Node, error) {
	n, ok := r.nodes[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	cp := *n
	return &cp, nil
}
func (r *handlerNodeRepo) List(context.Context) ([]domain.Node, error)        { return nil, nil }
func (r *handlerNodeRepo) ListEnabled(context.Context) ([]domain.Node, error) { return nil, nil }
func (r *handlerNodeRepo) Update(context.Context, *domain.Node) error         { return nil }
func (r *handlerNodeRepo) Delete(context.Context, int64) error                { return nil }
func (r *handlerNodeRepo) SetEnabled(context.Context, int64, bool) error      { return nil }
func (r *handlerNodeRepo) SetStatus(context.Context, int64, domain.NodeStatus, *time.Time, int64, string, string, float64, float64, int64, string) error {
	return nil
}

type handlerClientCache struct{}

func (handlerClientCache) GetByID(context.Context, int64) (*domain.Client, error) {
	return nil, repo.ErrNotFound
}
func (handlerClientCache) GetByRemote(context.Context, int64, string) (*domain.Client, error) {
	return nil, repo.ErrNotFound
}
func (handlerClientCache) UpsertRemote(context.Context, int64, string, int64, *domain.Client) error {
	return nil
}
func (handlerClientCache) Delete(context.Context, int64) error       { return nil }
func (handlerClientCache) DeleteMany(context.Context, []int64) error { return nil }
func (handlerClientCache) SetStatusMany(context.Context, []int64, domain.ClientStatus, bool) error {
	return nil
}
func (handlerClientCache) ResetTrafficMany(context.Context, []int64) error { return nil }

type handlerRemoteClienter struct{}

func (handlerRemoteClienter) Status(context.Context, *domain.Node) (*svcnode.RemoteStatus, time.Duration, error) {
	return nil, 0, nil
}
func (handlerRemoteClienter) Snapshot(context.Context, *domain.Node) (*svcnode.RemoteSnapshot, error) {
	return nil, nil
}
func (handlerRemoteClienter) CreateInbound(_ context.Context, _ *domain.Node, in svcnode.RemoteInboundRequest) (*svcnode.RemoteInbound, error) {
	return &svcnode.RemoteInbound{ID: "99", Remark: in.Remark, Protocol: domain.Protocol(in.Protocol), Port: in.Port, TLS: domain.TLSMode(in.TLS)}, nil
}
func (handlerRemoteClienter) UpdateInbound(context.Context, *domain.Node, string, svcnode.RemoteInboundRequest) (*svcnode.RemoteInbound, error) {
	return nil, nil
}
func (handlerRemoteClienter) DeleteInbound(context.Context, *domain.Node, string) error {
	return nil
}
func (handlerRemoteClienter) ToggleInbound(context.Context, *domain.Node, string) (*svcnode.RemoteInbound, error) {
	return nil, nil
}
func (handlerRemoteClienter) CreateClient(context.Context, *domain.Node, svcnode.RemoteClientCreateRequest) (*svcnode.RemoteClient, error) {
	return nil, nil
}
func (handlerRemoteClienter) UpdateClient(context.Context, *domain.Node, string, svcnode.RemoteClientUpdateRequest) (*svcnode.RemoteClient, error) {
	return nil, nil
}
func (handlerRemoteClienter) DeleteClient(context.Context, *domain.Node, string) error { return nil }
func (handlerRemoteClienter) ResetClientTraffic(context.Context, *domain.Node, string) (*svcnode.RemoteClient, error) {
	return nil, nil
}
func (handlerRemoteClienter) SetClientStatus(context.Context, *domain.Node, string, domain.ClientStatus) (*svcnode.RemoteClient, error) {
	return nil, nil
}
func (handlerRemoteClienter) BulkDeleteClients(_ context.Context, _ *domain.Node, ids []string) (*svcnode.RemoteClientBulkResponse, error) {
	return handlerBulkSuccess(ids), nil
}
func (handlerRemoteClienter) BulkResetClientTraffic(_ context.Context, _ *domain.Node, ids []string) (*svcnode.RemoteClientBulkResponse, error) {
	return handlerBulkSuccess(ids), nil
}
func (handlerRemoteClienter) BulkSetClientStatus(_ context.Context, _ *domain.Node, ids []string, _ domain.ClientStatus) (*svcnode.RemoteClientBulkResponse, error) {
	return handlerBulkSuccess(ids), nil
}

func handlerBulkSuccess(ids []string) *svcnode.RemoteClientBulkResponse {
	results := make([]svcnode.RemoteClientBulkResult, len(ids))
	for i, id := range ids {
		results[i] = svcnode.RemoteClientBulkResult{ID: id, OK: true}
	}
	return &svcnode.RemoteClientBulkResponse{Results: results}
}

func ptrInt64(v int64) *int64 {
	return &v
}
