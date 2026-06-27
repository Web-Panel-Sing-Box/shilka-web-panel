package node_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/services/node"
)

func TestHTTPClientBlocksPrivateAddressByDefault(t *testing.T) {
	client := node.NewHTTPClient()
	n := &domain.Node{
		Scheme:         "http",
		Address:        "127.0.0.1",
		Port:           8080,
		APITokenSecret: "secret",
	}

	_, _, err := client.Status(context.Background(), n)
	if !errors.Is(err, node.ErrUnsafeAddress) {
		t.Fatalf("expected unsafe address error, got %v", err)
	}
}

func TestHTTPClientAllowsPrivateAddressWhenExplicit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing bearer token")
		}
		_ = json.NewEncoder(w).Encode(node.RemoteStatus{PanelVersion: "shilka-test"})
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, portRaw, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatal(err)
	}

	client := node.NewHTTPClient()
	n := &domain.Node{
		Scheme:              u.Scheme,
		Address:             host,
		Port:                port,
		APITokenSecret:      "secret",
		AllowPrivateAddress: true,
	}

	status, _, err := client.Status(context.Background(), n)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.PanelVersion != "shilka-test" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestHTTPClientRequiresTrustedTLSByDefault(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(node.RemoteStatus{PanelVersion: "shilka-test"})
	}))
	defer srv.Close()

	n := nodeFromServerURL(t, srv.URL)
	client := node.NewHTTPClient()
	if _, _, err := client.Status(context.Background(), n); err == nil {
		t.Fatal("expected TLS verification error")
	}
}

func TestHTTPClientSkipsTLSVerificationWhenExplicit(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing bearer token")
		}
		_ = json.NewEncoder(w).Encode(node.RemoteStatus{PanelVersion: "shilka-test"})
	}))
	defer srv.Close()

	n := nodeFromServerURL(t, srv.URL)
	n.SkipTLSVerify = true

	client := node.NewHTTPClient()
	status, _, err := client.Status(context.Background(), n)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.PanelVersion != "shilka-test" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestHTTPClientCreateInboundUsesBasePathBearerAndJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/panel/api/node/v1/inbounds" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing bearer token")
		}
		var req node.RemoteInboundRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Remark != "edge" || req.ACMEDomain != "vpn.example.com" {
			t.Fatalf("request = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(node.RemoteInbound{ID: "42", Remark: req.Remark})
	}))
	defer srv.Close()

	n := nodeFromServerURL(t, srv.URL)
	n.BasePath = "/panel"
	client := node.NewHTTPClient()
	got, err := client.CreateInbound(context.Background(), n, node.RemoteInboundRequest{
		Remark:     "edge",
		Protocol:   "hysteria2",
		Port:       8443,
		TLS:        "tls",
		ACMEDomain: "vpn.example.com",
	})
	if err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	if got.ID != "42" || got.Remark != "edge" {
		t.Fatalf("response = %+v", got)
	}
}

func TestHTTPClientSetClientStatusSendsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/node/v1/clients/9/status" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var req node.RemoteClientStatusRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Status != string(domain.ClientStatusDisabled) {
			t.Fatalf("status = %q", req.Status)
		}
		_ = json.NewEncoder(w).Encode(node.RemoteClient{ID: "9", Status: domain.ClientStatusDisabled})
	}))
	defer srv.Close()

	client := node.NewHTTPClient()
	got, err := client.SetClientStatus(context.Background(), nodeFromServerURL(t, srv.URL), "9", domain.ClientStatusDisabled)
	if err != nil {
		t.Fatalf("set status: %v", err)
	}
	if got.Status != domain.ClientStatusDisabled {
		t.Fatalf("response = %+v", got)
	}
}

func TestHTTPClientBulkDeleteUsesOneRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/api/node/v1/clients/bulk/delete" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var req node.RemoteClientBulkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.IDs) != 2 || req.IDs[0] != "9" || req.IDs[1] != "10" {
			t.Fatalf("ids = %v", req.IDs)
		}
		_ = json.NewEncoder(w).Encode(node.RemoteClientBulkResponse{Results: []node.RemoteClientBulkResult{
			{ID: "9", OK: true},
			{ID: "10", Error: "not found"},
		}})
	}))
	defer srv.Close()

	response, err := node.NewHTTPClient().BulkDeleteClients(
		context.Background(), nodeFromServerURL(t, srv.URL), []string{"9", "10"},
	)
	if err != nil {
		t.Fatalf("BulkDeleteClients: %v", err)
	}
	if requests != 1 || len(response.Results) != 2 || !response.Results[0].OK || response.Results[1].Error != "not found" {
		t.Fatalf("requests = %d, response = %+v", requests, response)
	}
}

func TestHTTPClientRemoteStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()

	client := node.NewHTTPClient()
	err := client.DeleteClient(context.Background(), nodeFromServerURL(t, srv.URL), "9")
	if !node.IsRemoteStatus(err, http.StatusNotFound) {
		t.Fatalf("expected 404 remote status error, got %v", err)
	}
}

func TestHTTPClientClassifiesRefusedConnectionAsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	n := nodeFromServerURL(t, srv.URL)
	srv.Close() // free the port so connections are refused

	client := node.NewHTTPClient()
	_, _, err := client.Status(context.Background(), n)
	if !errors.Is(err, node.ErrNodeUnreachable) {
		t.Fatalf("expected ErrNodeUnreachable, got %v", err)
	}
	var ue *node.UnreachableError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *node.UnreachableError, got %T (%v)", err, err)
	}
	if ue.Timeout {
		t.Fatalf("connection refused must not be classified as timeout: %+v", ue)
	}
}

func TestHTTPClientClassifiesTimeoutAsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(node.RemoteStatus{PanelVersion: "shilka-test"})
	}))
	defer srv.Close()
	n := nodeFromServerURL(t, srv.URL)

	// A deadline already in the past forces context.DeadlineExceeded from Do.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()

	client := node.NewHTTPClient()
	_, _, err := client.Status(ctx, n)
	if !errors.Is(err, node.ErrNodeUnreachable) {
		t.Fatalf("expected ErrNodeUnreachable, got %v", err)
	}
	var ue *node.UnreachableError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *node.UnreachableError, got %T (%v)", err, err)
	}
	if !ue.Timeout || ue.Detail != "timeout" {
		t.Fatalf("expected timeout classification, got %+v", ue)
	}
}

func nodeFromServerURL(t *testing.T, rawURL string) *domain.Node {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	host, portRaw, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatal(err)
	}
	return &domain.Node{
		Scheme:              u.Scheme,
		Address:             host,
		Port:                port,
		APITokenSecret:      "secret",
		AllowPrivateAddress: true,
	}
}
