package domain

import "time"

const (
	NotificationEventQuota   = "quota"
	NotificationEventExpiry  = "expiry"
	NotificationEventApply   = "apply_failure"
	NotificationEventCheck   = "check_failure"
	NotificationEventReload  = "reload_failure"
	NotificationEventStart   = "core_start_failure"
	NotificationEventStop    = "core_stop_failure"
	NotificationEventRestart = "core_restart_failure"
)

const (
	NotificationLevelWarning  = 1
	NotificationLevelTerminal = 2
)

// NotificationSettings contains provider credentials and delivery policy.
// It is persisted separately from generic panel settings so secrets cannot be
// returned by GET /api/settings.
type NotificationSettings struct {
	SMTPEnabled         bool
	SMTPHost            string
	SMTPPort            int
	SMTPMode            string
	SMTPFrom            string
	SMTPRecipients      []string
	SMTPUsername        string
	SMTPPassword        string
	SMTPTimeoutSec      int
	TelegramEnabled     bool
	TelegramAPIBase     string
	TelegramBotToken    string
	TelegramChatID      string
	TelegramTimeoutSec  int
	QuotaWarningPercent int
	ExpiryWarningHours  int
	LastTestChannel     string
	LastTestOK          bool
	LastTestMessage     string
	LastTestAt          *time.Time
	UpdatedAt           time.Time
}

// NotificationEventState survives restarts and suppresses repeated events.
type NotificationEventState struct {
	SubjectType string
	SubjectID   string
	EventType   string
	Fingerprint string
	Level       int
	LastSentAt  time.Time
}
