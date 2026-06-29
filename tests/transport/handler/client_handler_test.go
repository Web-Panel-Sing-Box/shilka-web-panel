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

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
	svcclient "sing-box-web-panel/internal/services/client"
	svcnode "sing-box-web-panel/internal/services/node"
	"sing-box-web-panel/internal/transport/handler"
)

type bulkHandlerRepo struct {
	items map[int64]*domain.Client
}

func newBulkHandlerRepo() *bulkHandlerRepo {
	nodeID := int64(7)
	return &bulkHandlerRepo{items: map[int64]*domain.Client{
		1: {ID: 1, Name: "local", Status: domain.ClientStatusActive, Enabled: true},
		2: {ID: 2, NodeID: &nodeID, RemoteID: "20", Name: "remote", Status: domain.ClientStatusActive, Enabled: true},
	}}
}

func (r *bulkHandlerRepo) Create(context.Context, *domain.Client) error { return nil }
func (r *bulkHandlerRepo) GetByID(_ context.Context, id int64) (*domain.Client, error) {
	c, ok := r.items[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	cp := *c
	return &cp, nil
}
func (r *bulkHandlerRepo) GetBySubToken(context.Context, string) (*domain.Client, error) {
	return nil, repo.ErrNotFound
}
func (r *bulkHandlerRepo) GetByRemote(_ context.Context, nodeID int64, remoteID string) (*domain.Client, error) {
	for _, client := range r.items {
		if client.NodeID != nil && *client.NodeID == nodeID && client.RemoteID == remoteID {
			cp := *client
			return &cp, nil
		}
	}
	return nil, repo.ErrNotFound
}
func (r *bulkHandlerRepo) UpsertRemote(context.Context, int64, string, int64, *domain.Client) error {
	return nil
}
func (r *bulkHandlerRepo) List(context.Context) ([]domain.Client, error) { return nil, nil }
func (r *bulkHandlerRepo) ListByInbound(context.Context, int64) ([]domain.Client, error) {
	return nil, nil
}
func (r *bulkHandlerRepo) Update(context.Context, *domain.Client) error { return nil }
func (r *bulkHandlerRepo) Delete(_ context.Context, id int64) error {
	delete(r.items, id)
	return nil
}
func (r *bulkHandlerRepo) SetStatus(context.Context, int64, domain.ClientStatus, bool) error {
	return nil
}
func (r *bulkHandlerRepo) ResetTraffic(context.Context, int64) error { return nil }
func (r *bulkHandlerRepo) DeleteMany(_ context.Context, ids []int64) error {
	for _, id := range ids {
		delete(r.items, id)
	}
	return nil
}
func (r *bulkHandlerRepo) SetStatusMany(context.Context, []int64, domain.ClientStatus, bool) error {
	return nil
}
func (r *bulkHandlerRepo) ResetTrafficMany(context.Context, []int64) error { return nil }

type bulkHandlerInbounds struct{}

func (bulkHandlerInbounds) GetByID(context.Context, int64) (*domain.Inbound, error) {
	return &domain.Inbound{}, nil
}

type bulkAPIResponse struct {
	Results []struct {
		ID    string `json:"id"`
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	} `json:"results"`
}

func TestClientBulkDeletePreservesOrderDeduplicatesAndReturnsPartialFailures(t *testing.T) {
	mux := newBulkClientMux(newBulkHandlerRepo())
	req := httptest.NewRequest(http.MethodPost, "/api/clients/bulk/delete", bytes.NewBufferString(`{"ids":["1","1","2","999"]}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response bulkAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Results) != 3 {
		t.Fatalf("results = %+v", response.Results)
	}
	if response.Results[0].ID != "1" || !response.Results[0].OK {
		t.Fatalf("first result = %+v", response.Results[0])
	}
	if response.Results[1].ID != "2" || response.Results[1].OK || response.Results[1].Error != "invalid client" {
		t.Fatalf("second result = %+v", response.Results[1])
	}
	if response.Results[2].ID != "999" || response.Results[2].OK || response.Results[2].Error != "not found" {
		t.Fatalf("third result = %+v", response.Results[2])
	}
}

func TestClientBulkRejectsInvalidIDAndStatus(t *testing.T) {
	mux := newBulkClientMux(newBulkHandlerRepo())
	for _, tc := range []struct {
		path string
		body string
	}{
		{path: "/api/clients/bulk/delete", body: `{"ids":["0"]}`},
		{path: "/api/clients/bulk/set-status", body: `{"ids":["1"],"status":"expired"}`},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, body = %s", tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestClientBulkDeleteIsolatesLocalAndRemoteNodeFailures(t *testing.T) {
	repository := newBulkHandlerRepo()
	node7 := int64(7)
	node8 := int64(8)
	repository.items[3] = &domain.Client{ID: 3, NodeID: &node8, RemoteID: "30", Name: "remote-down"}
	remote := &nodeFakeRemote{
		bulkDeleteCalls: map[int64]int{},
		bulkDeleteErrs: map[int64]error{
			node8: &svcnode.UnreachableError{Detail: "timeout", Timeout: true},
		},
	}
	nodeService := svcnode.NewService(
		&nodeFakeRepo{items: map[int64]*domain.Node{
			node7: {ID: node7, Enabled: true, APITokenSecret: "secret"},
			node8: {ID: node8, Enabled: true, APITokenSecret: "secret"},
		}},
		nil,
		repository,
		remote,
	)
	service := svcclient.NewService(repository, bulkHandlerInbounds{}, nil)
	clientHandler := handler.NewClientHandler(service, "", slog.New(slog.NewTextHandler(io.Discard, nil)), nodeService)
	mux := http.NewServeMux()
	clientHandler.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/clients/bulk/delete", bytes.NewBufferString(`{"ids":["2","1","3"]}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response bulkAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 3 || !response.Results[0].OK || !response.Results[1].OK || response.Results[2].Error != "node unreachable" {
		t.Fatalf("results = %+v", response.Results)
	}
	if remote.bulkDeleteCalls[node7] != 1 || remote.bulkDeleteCalls[node8] != 1 {
		t.Fatalf("remote calls = %+v", remote.bulkDeleteCalls)
	}
}

func newBulkClientMux(repository *bulkHandlerRepo) *http.ServeMux {
	service := svcclient.NewService(repository, bulkHandlerInbounds{}, nil)
	clientHandler := handler.NewClientHandler(service, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	clientHandler.Register(mux)
	return mux
}
