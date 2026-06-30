package notification_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
	"sing-box-web-panel/internal/services/notification"
)

type fakeRepo struct {
	mu       sync.Mutex
	settings domain.NotificationSettings
	states   map[string]domain.NotificationEventState
}

func newRepo() *fakeRepo {
	return &fakeRepo{settings: domain.NotificationSettings{
		SMTPPort: 587, SMTPMode: "starttls", SMTPTimeoutSec: 10,
		TelegramAPIBase: "https://api.telegram.org", TelegramTimeoutSec: 10,
		QuotaWarningPercent: 80, ExpiryWarningHours: 24,
	}, states: make(map[string]domain.NotificationEventState)}
}

func (r *fakeRepo) GetSettings(context.Context) (domain.NotificationSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := r.settings
	copy.SMTPRecipients = append([]string(nil), r.settings.SMTPRecipients...)
	return copy, nil
}
func (r *fakeRepo) SaveSettings(_ context.Context, settings domain.NotificationSettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settings = settings
	return nil
}
func (r *fakeRepo) SaveLastTest(_ context.Context, channel string, ok bool, message string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settings.LastTestChannel, r.settings.LastTestOK, r.settings.LastTestMessage, r.settings.LastTestAt = channel, ok, message, &at
	return nil
}
func stateKey(subjectType, subjectID, eventType string) string {
	return subjectType + "/" + subjectID + "/" + eventType
}
func (r *fakeRepo) GetEventState(_ context.Context, subjectType, subjectID, eventType string) (domain.NotificationEventState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.states[stateKey(subjectType, subjectID, eventType)]
	if !ok {
		return state, repo.ErrNotFound
	}
	return state, nil
}
func (r *fakeRepo) SaveEventState(_ context.Context, state domain.NotificationEventState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states[stateKey(state.SubjectType, state.SubjectID, state.EventType)] = state
	return nil
}
func (r *fakeRepo) DeleteEventState(_ context.Context, subjectType, subjectID, eventType string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.states, stateKey(subjectType, subjectID, eventType))
	return nil
}

type fakeSMTP struct {
	mu       sync.Mutex
	messages []notification.Message
	err      error
}

func (s *fakeSMTP) Send(_ context.Context, _ domain.NotificationSettings, message notification.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, message)
	return s.err
}
func (s *fakeSMTP) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.messages) }
func (s *fakeSMTP) last() notification.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messages[len(s.messages)-1]
}

type fakeTelegram struct{}

func (fakeTelegram) Send(context.Context, domain.NotificationSettings, notification.Message) error {
	return nil
}

type countingTelegram struct{ sent chan struct{} }

func (s countingTelegram) Send(context.Context, domain.NotificationSettings, notification.Message) error {
	s.sent <- struct{}{}
	return nil
}

func logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSettingsMaskAndSecretUpdateSemantics(t *testing.T) {
	repository := newRepo()
	repository.settings.SMTPPassword = "old-password"
	repository.settings.TelegramBotToken = "old-token"
	service := notification.New(repository, &fakeSMTP{}, fakeTelegram{}, logger())

	view, err := service.Update(context.Background(), notification.Update{
		SMTP:                notification.SMTPUpdate{Host: "smtp.example.com", Port: 587, Mode: "starttls", From: "sender@example.com", Recipients: []string{"admin@example.com"}, Username: "user", TimeoutSec: 10},
		Telegram:            notification.TelegramUpdate{APIBase: "https://api.telegram.org", ChatID: "42", TimeoutSec: 10},
		QuotaWarningPercent: 80, ExpiryWarningHours: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !view.SMTP.PasswordConfigured || !view.Telegram.BotTokenConfigured {
		t.Fatal("configured flags were lost")
	}
	stored, _ := repository.GetSettings(context.Background())
	if stored.SMTPPassword != "old-password" || stored.TelegramBotToken != "old-token" {
		t.Fatal("empty secret did not preserve stored value")
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "old-password") || strings.Contains(string(encoded), "old-token") {
		t.Fatal("view leaked secret")
	}

	_, err = service.Update(context.Background(), notification.Update{
		SMTP:                notification.SMTPUpdate{Host: "smtp.example.com", Port: 587, Mode: "starttls", From: "sender@example.com", Recipients: []string{"admin@example.com"}, ClearPassword: true, TimeoutSec: 10},
		Telegram:            notification.TelegramUpdate{APIBase: "https://api.telegram.org", ChatID: "42", ClearBotToken: true, TimeoutSec: 10},
		QuotaWarningPercent: 80, ExpiryWarningHours: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, _ = repository.GetSettings(context.Background())
	if stored.SMTPPassword != "" || stored.TelegramBotToken != "" {
		t.Fatal("explicit clear did not remove secret")
	}
}

func TestQuotaDedupePersistsAndResets(t *testing.T) {
	repository := newRepo()
	repository.settings.SMTPEnabled = true
	sender := &fakeSMTP{}
	service := notification.New(repository, sender, fakeTelegram{}, logger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.Run(ctx)

	client := domain.Client{ID: 7, Name: "alice", TotalQuota: 100, UsedDown: 80}
	if err := service.ObserveClient(ctx, client, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := service.ObserveClient(ctx, client, time.Now()); err != nil {
		t.Fatal(err)
	}
	waitCount(t, sender, 1)

	secondSender := &fakeSMTP{}
	second := notification.New(repository, secondSender, fakeTelegram{}, logger())
	if err := second.ObserveClient(ctx, client, time.Now()); err != nil {
		t.Fatal(err)
	}
	if secondSender.count() != 0 {
		t.Fatal("persistent state did not dedupe after service restart")
	}

	client.UsedDown = 100
	if err := service.ObserveClient(ctx, client, time.Now()); err != nil {
		t.Fatal(err)
	}
	waitCount(t, sender, 2)
	client.UsedDown = 10
	if err := service.ObserveClient(ctx, client, time.Now()); err != nil {
		t.Fatal(err)
	}
	client.UsedDown = 80
	if err := service.ObserveClient(ctx, client, time.Now()); err != nil {
		t.Fatal(err)
	}
	waitCount(t, sender, 3)
}

func TestCoreErrorCooldownAndRedaction(t *testing.T) {
	repository := newRepo()
	repository.settings.SMTPEnabled = true
	repository.settings.SMTPPassword = "smtp-secret"
	repository.settings.TelegramBotToken = "123456:bot-secret"
	sender := &fakeSMTP{}
	service := notification.New(repository, sender, fakeTelegram{}, logger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.Run(ctx)

	cause := errors.New("failed https://user:pass@example.test/x?token=123456:bot-secret password=smtp-secret")
	service.NotifyFailure(ctx, domain.NotificationEventApply, cause)
	service.NotifyFailure(ctx, domain.NotificationEventApply, cause)
	waitCount(t, sender, 1)
	body := sender.last().Body
	if strings.Contains(body, "smtp-secret") || strings.Contains(body, "bot-secret") || strings.Contains(body, "user:pass") {
		t.Fatalf("secret leaked: %s", body)
	}
	service.NotifyFailure(ctx, domain.NotificationEventApply, errors.New("different failure"))
	waitCount(t, sender, 2)
}

func TestSMTPFailureDoesNotBlockTelegram(t *testing.T) {
	repository := newRepo()
	repository.settings.SMTPEnabled = true
	repository.settings.TelegramEnabled = true
	smtp := &fakeSMTP{err: errors.New("smtp down")}
	telegram := countingTelegram{sent: make(chan struct{}, 1)}
	service := notification.New(repository, smtp, telegram, logger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.Run(ctx)
	service.NotifyFailure(ctx, domain.NotificationEventApply, errors.New("apply failed"))
	select {
	case <-telegram.sent:
	case <-time.After(time.Second):
		t.Fatal("Telegram was blocked by SMTP failure")
	}
}

func TestTelegramLoopbackTestAndPersistedResult(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	repository := newRepo()
	repository.settings.TelegramAPIBase = server.URL
	repository.settings.TelegramBotToken = "123:token"
	repository.settings.TelegramChatID = "99"
	service := notification.New(repository, &fakeSMTP{}, notification.NewTelegramClient(), logger())
	result, err := service.Test(context.Background(), "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || path != "/bot123:token/sendMessage" {
		t.Fatalf("result=%+v path=%q", result, path)
	}
	view, _ := service.Get(context.Background())
	if !view.LastTest.OK || view.LastTest.Channel != "telegram" {
		t.Fatalf("last test not persisted: %+v", view.LastTest)
	}
}

func TestTelegramRejectsNonLoopbackHTTP(t *testing.T) {
	repository := newRepo()
	repository.settings.TelegramAPIBase = "http://example.com"
	repository.settings.TelegramBotToken = "123:token"
	repository.settings.TelegramChatID = "99"
	service := notification.New(repository, &fakeSMTP{}, fakeTelegram{}, logger())
	_, err := service.Test(context.Background(), "telegram")
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("error=%v", err)
	}
}

func TestRedactSensitiveValues(t *testing.T) {
	result := notification.Redact("https://user:pass@example.test/a?password=one&api_key=two bot123456:abc", "one")
	for _, secret := range []string{"user:pass", "password=one", "api_key=two", "bot123456:abc"} {
		if strings.Contains(result, secret) {
			t.Fatalf("%q leaked in %q", secret, result)
		}
	}
}

func waitCount(t *testing.T, sender *fakeSMTP, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if sender.count() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("message count=%d want=%d", sender.count(), want)
}
