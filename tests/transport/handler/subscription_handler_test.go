package handler_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
	"sing-box-web-panel/internal/transport/handler"
)

type clientLinksResponse struct {
	ShareLink string `json:"shareLink"`
	QRPng     string `json:"qrPng"`
}

type subscriptionFakeClients struct {
	byToken map[string]*domain.Client
	byID    map[int64]*domain.Client
}

func (r subscriptionFakeClients) GetBySubToken(_ context.Context, token string) (*domain.Client, error) {
	if c, ok := r.byToken[token]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, repo.ErrNotFound
}

func (r subscriptionFakeClients) GetByID(_ context.Context, id int64) (*domain.Client, error) {
	if c, ok := r.byID[id]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, repo.ErrNotFound
}

type subscriptionFakeInbounds struct {
	byID map[int64]*domain.Inbound
}

func (r subscriptionFakeInbounds) GetByID(_ context.Context, id int64) (*domain.Inbound, error) {
	if ib, ok := r.byID[id]; ok {
		cp := *ib
		return &cp, nil
	}
	return nil, repo.ErrNotFound
}

func testSubscriptionMux(ib *domain.Inbound, c *domain.Client) *http.ServeMux {
	clients := subscriptionFakeClients{
		byToken: map[string]*domain.Client{c.SubToken: c},
		byID:    map[int64]*domain.Client{c.ID: c},
	}
	inbounds := subscriptionFakeInbounds{byID: map[int64]*domain.Inbound{ib.ID: ib}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handler.NewSubscriptionHandler(clients, inbounds, nil, "", "", log)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func TestSubscriptionNaiveJSONSelfSignedReturns400(t *testing.T) {
	ib := &domain.Inbound{
		ID: 7, Protocol: domain.ProtocolNaive, Port: 38119,
		TLS: domain.TLSModeTLS, SNI: "panel.example",
	}
	c := &domain.Client{
		ID: 9, InboundID: 7, Name: "carol", Password: "pw",
		Status: domain.ClientStatusActive, Enabled: true, SubToken: "tok",
	}
	mux := testSubscriptionMux(ib, c)

	req := httptest.NewRequest(http.MethodGet, "/sub/tok?format=json", nil)
	req.Host = "panel.example"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "naive json subscription requires trusted TLS") {
		t.Fatalf("body = %q, want trusted TLS error", rec.Body.String())
	}
}

func TestSubscriptionNaivePlainStillReturnsShareLink(t *testing.T) {
	ib := &domain.Inbound{
		ID: 7, Protocol: domain.ProtocolNaive, Port: 38119,
		TLS: domain.TLSModeTLS,
	}
	c := &domain.Client{
		ID: 9, InboundID: 7, Name: "carol", Password: "pw",
		Status: domain.ClientStatusActive, Enabled: true, SubToken: "tok",
	}
	mux := testSubscriptionMux(ib, c)

	req := httptest.NewRequest(http.MethodGet, "/sub/tok?format=plain", nil)
	req.Host = "panel.example"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Body.String(), "naive+https://carol:pw@panel.example:38119") {
		t.Fatalf("link = %q", rec.Body.String())
	}
}

func TestClientLinksReturnsPNGQRForSupportedProtocols(t *testing.T) {
	tests := []struct {
		name     string
		protocol domain.Protocol
	}{
		{name: "vless", protocol: domain.ProtocolVLESS},
		{name: "hysteria2", protocol: domain.ProtocolHysteria2},
		{name: "naive", protocol: domain.ProtocolNaive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ib := &domain.Inbound{
				ID: 7, Protocol: tt.protocol, Port: 443,
				TLS: domain.TLSModeTLS, SNI: "panel.example",
			}
			c := &domain.Client{
				ID: 9, InboundID: 7, Name: "carol", UUID: "81514c35-8f9a-4785-9afc-013bb4f0f13e",
				Password: "pw", Status: domain.ClientStatusActive, Enabled: true, SubToken: "tok",
			}
			mux := testSubscriptionMux(ib, c)
			req := httptest.NewRequest(http.MethodGet, "/api/clients/9/links", nil)
			req.Host = "panel.example"
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
			var body clientLinksResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.ShareLink == "" {
				t.Fatal("shareLink is empty")
			}
			const prefix = "data:image/png;base64,"
			if !strings.HasPrefix(body.QRPng, prefix) {
				t.Fatalf("qrPng prefix = %q", body.QRPng)
			}
			png, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(body.QRPng, prefix))
			if err != nil {
				t.Fatalf("decode qr PNG: %v", err)
			}
			if len(png) < 8 || string(png[:8]) != "\x89PNG\r\n\x1a\n" {
				t.Fatalf("qrPng is not a PNG: %x", png[:min(len(png), 8)])
			}
		})
	}
}

func TestPublicSubscriptionRejectsInactiveClients(t *testing.T) {
	for _, status := range []domain.ClientStatus{domain.ClientStatusDisabled, domain.ClientStatusExpired} {
		t.Run(string(status), func(t *testing.T) {
			ib := &domain.Inbound{ID: 7, Protocol: domain.ProtocolVLESS, Port: 443}
			c := &domain.Client{
				ID: 9, InboundID: 7, Name: "carol", UUID: "81514c35-8f9a-4785-9afc-013bb4f0f13e",
				Status: status, Enabled: true, SubToken: "tok",
			}
			mux := testSubscriptionMux(ib, c)
			for _, path := range []string{"/sub/tok?format=plain", "/api/subscription/tok/meta"} {
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				if rec.Code != http.StatusForbidden {
					t.Fatalf("%s status = %d, want 403", path, rec.Code)
				}
			}
		})
	}
}
