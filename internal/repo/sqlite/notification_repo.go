package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
)

type NotificationRepo struct{ db *sql.DB }

func NewNotificationRepo(db *sql.DB) *NotificationRepo { return &NotificationRepo{db: db} }

func (r *NotificationRepo) GetSettings(ctx context.Context) (domain.NotificationSettings, error) {
	var s domain.NotificationSettings
	var recipients string
	err := r.db.QueryRowContext(ctx, `SELECT smtp_enabled, smtp_host, smtp_port, smtp_mode, smtp_from,
		smtp_recipients_json, smtp_username, smtp_password, smtp_timeout_sec,
		telegram_enabled, telegram_api_base, telegram_bot_token, telegram_chat_id, telegram_timeout_sec,
		quota_warning_percent, expiry_warning_hours, last_test_channel, last_test_ok,
		last_test_message, last_test_at, updated_at FROM notification_settings WHERE id = 1`).Scan(
		&s.SMTPEnabled, &s.SMTPHost, &s.SMTPPort, &s.SMTPMode, &s.SMTPFrom,
		&recipients, &s.SMTPUsername, &s.SMTPPassword, &s.SMTPTimeoutSec,
		&s.TelegramEnabled, &s.TelegramAPIBase, &s.TelegramBotToken, &s.TelegramChatID, &s.TelegramTimeoutSec,
		&s.QuotaWarningPercent, &s.ExpiryWarningHours, &s.LastTestChannel, &s.LastTestOK,
		&s.LastTestMessage, &s.LastTestAt, &s.UpdatedAt)
	if err != nil {
		return s, fmt.Errorf("get notification settings: %w", err)
	}
	if err := json.Unmarshal([]byte(recipients), &s.SMTPRecipients); err != nil {
		return s, fmt.Errorf("decode smtp recipients: %w", err)
	}
	return s, nil
}

func (r *NotificationRepo) SaveSettings(ctx context.Context, s domain.NotificationSettings) error {
	recipients, err := json.Marshal(s.SMTPRecipients)
	if err != nil {
		return fmt.Errorf("encode smtp recipients: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `UPDATE notification_settings SET
		smtp_enabled=?, smtp_host=?, smtp_port=?, smtp_mode=?, smtp_from=?, smtp_recipients_json=?,
		smtp_username=?, smtp_password=?, smtp_timeout_sec=?, telegram_enabled=?, telegram_api_base=?,
		telegram_bot_token=?, telegram_chat_id=?, telegram_timeout_sec=?, quota_warning_percent=?,
		expiry_warning_hours=?, updated_at=CURRENT_TIMESTAMP WHERE id=1`,
		s.SMTPEnabled, s.SMTPHost, s.SMTPPort, s.SMTPMode, s.SMTPFrom, string(recipients),
		s.SMTPUsername, s.SMTPPassword, s.SMTPTimeoutSec, s.TelegramEnabled, s.TelegramAPIBase,
		s.TelegramBotToken, s.TelegramChatID, s.TelegramTimeoutSec, s.QuotaWarningPercent,
		s.ExpiryWarningHours)
	if err != nil {
		return fmt.Errorf("save notification settings: %w", err)
	}
	return nil
}

func (r *NotificationRepo) SaveLastTest(ctx context.Context, channel string, ok bool, message string, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE notification_settings SET last_test_channel=?, last_test_ok=?,
		last_test_message=?, last_test_at=?, updated_at=CURRENT_TIMESTAMP WHERE id=1`, channel, ok, message, at)
	if err != nil {
		return fmt.Errorf("save notification test: %w", err)
	}
	return nil
}

func (r *NotificationRepo) GetEventState(ctx context.Context, subjectType, subjectID, eventType string) (domain.NotificationEventState, error) {
	var s domain.NotificationEventState
	err := r.db.QueryRowContext(ctx, `SELECT subject_type, subject_id, event_type, fingerprint, level, last_sent_at
		FROM notification_event_state WHERE subject_type=? AND subject_id=? AND event_type=?`,
		subjectType, subjectID, eventType).Scan(&s.SubjectType, &s.SubjectID, &s.EventType, &s.Fingerprint, &s.Level, &s.LastSentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return s, repo.ErrNotFound
	}
	if err != nil {
		return s, fmt.Errorf("get notification event state: %w", err)
	}
	return s, nil
}

func (r *NotificationRepo) SaveEventState(ctx context.Context, s domain.NotificationEventState) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO notification_event_state
		(subject_type, subject_id, event_type, fingerprint, level, last_sent_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(subject_type, subject_id, event_type) DO UPDATE SET
		fingerprint=excluded.fingerprint, level=excluded.level, last_sent_at=excluded.last_sent_at`,
		s.SubjectType, s.SubjectID, s.EventType, s.Fingerprint, s.Level, s.LastSentAt)
	if err != nil {
		return fmt.Errorf("save notification event state: %w", err)
	}
	return nil
}

func (r *NotificationRepo) DeleteEventState(ctx context.Context, subjectType, subjectID, eventType string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM notification_event_state WHERE subject_type=? AND subject_id=? AND event_type=?`, subjectType, subjectID, eventType)
	if err != nil {
		return fmt.Errorf("delete notification event state: %w", err)
	}
	return nil
}
