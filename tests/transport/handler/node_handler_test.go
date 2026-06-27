package handler_test

import (
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
	svcnode "sing-box-web-panel/internal/services/node"
	"sing-box-web-panel/internal/transport/handler"
)

// nodeFakeRepo is a minimal in-memory node.Repo for handler tests.
type nodeFakeRepo struct {
	items map[int64]*domain.Node
}

func newNodeFakeRepo() *nodeFakeRepo {
	return &nodeFakeRepo{items: map[int64]*domain.Node{
		1: {ID: 1, Name: "edge", Scheme: "https", Address: "1.2.3.4", Port: 443, Enabled: true},
	}}
}

func (r *nodeFakeRepo) Create(context.Context, *domain.Node) error { return nil }

func (r *nodeFakeRepo) GetByID(_ context.Context, id int64) (*domain.Node, error) {
	if n, ok := r.items[id]; ok {
		cp := *n
		return &cp, nil
	}
	return nil, repo.ErrNotFound
}

func (r *nodeFakeRepo) List(context.Context) ([]domain.Node, error)        { return nil, nil }
func (r *nodeFakeRepo) ListEnabled(context.Context) ([]domain.Node, error) { return nil, nil }
func (r *nodeFakeRepo) Update(context.Context, *domain.Node) error         { return nil }
func (r *nodeFakeRepo) Delete(context.Context, int64) error                { return nil }
func (r *nodeFakeRepo) SetEnabled(context.Context, int64, bool) error      { return nil }

func (r *nodeFakeRepo) SetStatus(_ context.Context, id int64, status domain.NodeStatus, _ *time.Time, _ int64, _, _ string, _, _ float64, _ int64, lastErr string) error {
	if n, ok := r.items[id]; ok {
		n.Status = status
		n.LastError = lastErr
	}
	return nil
}

// nodeFakeRemote implements svcnode.RemoteClienter. Snapshot returns a
// configurable error to simulate an unreachable node; the remaining methods are
// unused stubs needed only to satisfy the interface.
type nodeFakeRemote struct {
	snapErr         error
	bulkDeleteCalls map[int64]int
	bulkDeleteErrs  map[int64]error
}

func (c *nodeFakeRemote) Status(context.Context, *domain.Node) (*svcnode.RemoteStatus, time.Duration, error) {
	return nil, 0, c.snapErr
}

func (c *nodeFakeRemote) Snapshot(context.Context, *domain.Node) (*svcnode.RemoteSnapshot, error) {
	return nil, c.snapErr
}

func (c *nodeFakeRemote) CreateInbound(context.Context, *domain.Node, svcnode.RemoteInboundRequest) (*svcnode.RemoteInbound, error) {
	return nil, nil
}

func (c *nodeFakeRemote) UpdateInbound(context.Context, *domain.Node, string, svcnode.RemoteInboundRequest) (*svcnode.RemoteInbound, error) {
	return nil, nil
}

func (c *nodeFakeRemote) DeleteInbound(context.Context, *domain.Node, string) error { return nil }

func (c *nodeFakeRemote) ToggleInbound(context.Context, *domain.Node, string) (*svcnode.RemoteInbound, error) {
	return nil, nil
}

func (c *nodeFakeRemote) CreateClient(context.Context, *domain.Node, svcnode.RemoteClientCreateRequest) (*svcnode.RemoteClient, error) {
	return nil, nil
}

func (c *nodeFakeRemote) UpdateClient(context.Context, *domain.Node, string, svcnode.RemoteClientUpdateRequest) (*svcnode.RemoteClient, error) {
	return nil, nil
}

func (c *nodeFakeRemote) DeleteClient(context.Context, *domain.Node, string) error { return nil }

func (c *nodeFakeRemote) ResetClientTraffic(context.Context, *domain.Node, string) (*svcnode.RemoteClient, error) {
	return nil, nil
}

func (c *nodeFakeRemote) SetClientStatus(context.Context, *domain.Node, string, domain.ClientStatus) (*svcnode.RemoteClient, error) {
	return nil, nil
}

func (c *nodeFakeRemote) BulkDeleteClients(_ context.Context, node *domain.Node, ids []string) (*svcnode.RemoteClientBulkResponse, error) {
	if c.bulkDeleteCalls != nil {
		c.bulkDeleteCalls[node.ID]++
	}
	if err := c.bulkDeleteErrs[node.ID]; err != nil {
		return nil, err
	}
	return nodeHandlerBulkSuccess(ids), nil
}

func (c *nodeFakeRemote) BulkResetClientTraffic(_ context.Context, _ *domain.Node, ids []string) (*svcnode.RemoteClientBulkResponse, error) {
	return nodeHandlerBulkSuccess(ids), nil
}

func (c *nodeFakeRemote) BulkSetClientStatus(_ context.Context, _ *domain.Node, ids []string, _ domain.ClientStatus) (*svcnode.RemoteClientBulkResponse, error) {
	return nodeHandlerBulkSuccess(ids), nil
}

func nodeHandlerBulkSuccess(ids []string) *svcnode.RemoteClientBulkResponse {
	results := make([]svcnode.RemoteClientBulkResult, len(ids))
	for i, id := range ids {
		results[i] = svcnode.RemoteClientBulkResult{ID: id, OK: true}
	}
	return &svcnode.RemoteClientBulkResponse{Results: results}
}

func testNodeMux(remote svcnode.RemoteClienter) *http.ServeMux {
	svc := svcnode.NewService(newNodeFakeRepo(), nil, nil, remote)
	h := handler.NewNodeHandler(svc, nil, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func TestNodeSyncTimeoutReturns504(t *testing.T) {
	mux := testNodeMux(&nodeFakeRemote{snapErr: &svcnode.UnreachableError{Detail: "timeout", Timeout: true}})

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/1/sync", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504; body: %s", rec.Code, rec.Body.String())
	}
	assertUnreachableBody(t, rec.Body.Bytes(), "timeout")
}

func TestNodeSyncUnreachableReturns502(t *testing.T) {
	mux := testNodeMux(&nodeFakeRemote{snapErr: &svcnode.UnreachableError{Detail: "connection refused"}})

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/1/sync", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body: %s", rec.Code, rec.Body.String())
	}
	assertUnreachableBody(t, rec.Body.Bytes(), "connection refused")
}

func assertUnreachableBody(t *testing.T, body []byte, wantDetail string) {
	t.Helper()
	var resp map[string]string
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal response: %v; body: %s", err, body)
	}
	if resp["error"] != "node unreachable" {
		t.Fatalf("error = %q, want %q", resp["error"], "node unreachable")
	}
	if resp["detail"] != wantDetail {
		t.Fatalf("detail = %q, want %q", resp["detail"], wantDetail)
	}
}
