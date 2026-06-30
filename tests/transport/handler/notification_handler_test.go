package handler_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sing-box-web-panel/internal/config"
	"sing-box-web-panel/internal/domain"
	sqliterepo "sing-box-web-panel/internal/repo/sqlite"
	"sing-box-web-panel/internal/services/notification"
	"sing-box-web-panel/internal/transport/handler"
)

type handlerSMTP struct{ count int }

func (s *handlerSMTP) Send(context.Context, domain.NotificationSettings, notification.Message) error {
	s.count++
	return nil
}

type handlerTelegram struct{}

func (handlerTelegram) Send(context.Context, domain.NotificationSettings, notification.Message) error {
	return nil
}

func TestNotificationSettingsAPIHidesSecretsAndTestsChannel(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sqliterepo.New(config.DBConfig{Path: t.TempDir() + "/panel.db", JournalMode: "wal", Synchronous: "normal", BusyTimeoutMS: 1000, TempStore: "memory", ForeignKeys: true}, log)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	smtp := &handlerSMTP{}
	service := notification.New(sqliterepo.NewNotificationRepo(db), smtp, handlerTelegram{}, log)
	mux := http.NewServeMux()
	handler.NewNotificationHandler(service, log).Register(mux)

	payload := `{"smtp":{"enabled":true,"host":"smtp.example.com","port":587,"mode":"starttls","from":"sender@example.com","recipients":["admin@example.com"],"username":"user","password":"smtp-secret","clearPassword":false,"timeoutSec":10},"telegram":{"enabled":false,"apiBase":"https://api.telegram.org","botToken":"bot-secret","clearBotToken":false,"chatId":"42","timeoutSec":10},"quotaWarningPercent":80,"expiryWarningHours":24}`
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/settings/notifications", bytes.NewBufferString(payload)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "smtp-secret") || strings.Contains(recorder.Body.String(), "bot-secret") {
		t.Fatal("PUT response leaked secret")
	}

	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/settings/notifications", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"passwordConfigured":true`) || !strings.Contains(recorder.Body.String(), `"botTokenConfigured":true`) {
		t.Fatalf("get body=%s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "smtp-secret") || strings.Contains(recorder.Body.String(), "bot-secret") {
		t.Fatal("GET response leaked secret")
	}

	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/settings/notifications/test", bytes.NewBufferString(`{"channel":"smtp"}`)))
	if recorder.Code != http.StatusOK || smtp.count != 1 {
		t.Fatalf("test status=%d count=%d body=%s", recorder.Code, smtp.count, recorder.Body.String())
	}
}
