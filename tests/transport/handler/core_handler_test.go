package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/services/singbox"
	"sing-box-web-panel/internal/transport/handler"
)

type fakeProcessManager struct {
	status  singbox.Status
	stopErr error
}

func (f fakeProcessManager) Start(context.Context) error   { return nil }
func (f fakeProcessManager) Stop(context.Context) error    { return f.stopErr }
func (f fakeProcessManager) Restart(context.Context) error { return nil }
func (f fakeProcessManager) Reload(context.Context) error  { return nil }
func (f fakeProcessManager) Status(context.Context) (singbox.Status, error) {
	return f.status, nil
}

type failureNotifier struct{ events []string }

func (f *failureNotifier) NotifyFailure(_ context.Context, eventType string, _ error) {
	f.events = append(f.events, eventType)
}
func (f *failureNotifier) RedactError(_ context.Context, text string) string {
	return strings.ReplaceAll(text, "secret", "[redacted]")
}

func TestCoreStatusIncludesLastError(t *testing.T) {
	pm := fakeProcessManager{status: singbox.Status{
		Running:   false,
		Version:   "sing-box 1.2.3",
		Uptime:    5 * time.Second,
		LastError: "clash-api: bind 127.0.0.1:9090: address already in use",
	}}
	h := handler.NewCoreHandler(pm, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/core/status", nil)
	h.Status(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["lastError"] != "clash-api: bind 127.0.0.1:9090: address already in use" {
		t.Fatalf("lastError = %v", body["lastError"])
	}
}

func TestCoreStopFailureNotifies(t *testing.T) {
	pm := fakeProcessManager{stopErr: errors.New("stop failed token=secret")}
	notifier := &failureNotifier{}
	h := handler.NewCoreHandler(pm, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "", notifier)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/core/stop", nil)
	h.Stop(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", rec.Code)
	}
	if len(notifier.events) != 1 || notifier.events[0] != domain.NotificationEventStop {
		t.Fatalf("events=%v", notifier.events)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("response leaked secret: %s", rec.Body.String())
	}
}
