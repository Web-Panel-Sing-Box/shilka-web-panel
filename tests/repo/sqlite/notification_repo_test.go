package sqlite_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"sing-box-web-panel/internal/config"
	"sing-box-web-panel/internal/domain"
	sqliterepo "sing-box-web-panel/internal/repo/sqlite"
)

func TestNotificationRepositoryMigrationAndPersistence(t *testing.T) {
	db, err := sqliterepo.New(config.DBConfig{
		Path: t.TempDir() + "/panel.db", JournalMode: "wal", Synchronous: "normal",
		BusyTimeoutMS: 1000, TempStore: "memory", ForeignKeys: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := sqliterepo.NewNotificationRepo(db)
	settings, err := repository.GetSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if settings.SMTPPort != 587 || settings.QuotaWarningPercent != 80 || settings.ExpiryWarningHours != 24 {
		t.Fatalf("defaults=%+v", settings)
	}

	settings.SMTPPassword = "secret"
	settings.SMTPRecipients = []string{"admin@example.com"}
	settings.TelegramBotToken = "token"
	if err := repository.SaveSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored.SMTPPassword != "secret" || len(stored.SMTPRecipients) != 1 || stored.TelegramBotToken != "token" {
		t.Fatalf("stored=%+v", stored)
	}

	state := domain.NotificationEventState{SubjectType: "client", SubjectID: "7", EventType: domain.NotificationEventQuota, Fingerprint: "100", Level: 1, LastSentAt: time.Now().UTC().Truncate(time.Second)}
	if err := repository.SaveEventState(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	got, err := repository.GetEventState(context.Background(), "client", "7", domain.NotificationEventQuota)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fingerprint != "100" || got.Level != 1 {
		t.Fatalf("state=%+v", got)
	}
	if err := repository.DeleteEventState(context.Background(), "client", "7", domain.NotificationEventQuota); err != nil {
		t.Fatal(err)
	}
}
