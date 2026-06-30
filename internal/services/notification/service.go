package notification

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
)

var ErrValidation = errors.New("notification validation")

const coreErrorCooldown = 5 * time.Minute

type Repository interface {
	GetSettings(context.Context) (domain.NotificationSettings, error)
	SaveSettings(context.Context, domain.NotificationSettings) error
	SaveLastTest(context.Context, string, bool, string, time.Time) error
	GetEventState(context.Context, string, string, string) (domain.NotificationEventState, error)
	SaveEventState(context.Context, domain.NotificationEventState) error
	DeleteEventState(context.Context, string, string, string) error
}

type SMTPSender interface {
	Send(context.Context, domain.NotificationSettings, Message) error
}

type TelegramSender interface {
	Send(context.Context, domain.NotificationSettings, Message) error
}

type Message struct {
	Subject string
	Body    string
}

type SMTPUpdate struct {
	Enabled       bool     `json:"enabled"`
	Host          string   `json:"host"`
	Port          int      `json:"port"`
	Mode          string   `json:"mode"`
	From          string   `json:"from"`
	Recipients    []string `json:"recipients"`
	Username      string   `json:"username"`
	Password      string   `json:"password"`
	ClearPassword bool     `json:"clearPassword"`
	TimeoutSec    int      `json:"timeoutSec"`
}

type TelegramUpdate struct {
	Enabled       bool   `json:"enabled"`
	APIBase       string `json:"apiBase"`
	BotToken      string `json:"botToken"`
	ClearBotToken bool   `json:"clearBotToken"`
	ChatID        string `json:"chatId"`
	TimeoutSec    int    `json:"timeoutSec"`
}

type Update struct {
	SMTP                SMTPUpdate     `json:"smtp"`
	Telegram            TelegramUpdate `json:"telegram"`
	QuotaWarningPercent int            `json:"quotaWarningPercent"`
	ExpiryWarningHours  int            `json:"expiryWarningHours"`
}

type SMTPView struct {
	Enabled            bool     `json:"enabled"`
	Host               string   `json:"host"`
	Port               int      `json:"port"`
	Mode               string   `json:"mode"`
	From               string   `json:"from"`
	Recipients         []string `json:"recipients"`
	Username           string   `json:"username"`
	PasswordConfigured bool     `json:"passwordConfigured"`
	TimeoutSec         int      `json:"timeoutSec"`
}

type TelegramView struct {
	Enabled            bool   `json:"enabled"`
	APIBase            string `json:"apiBase"`
	BotTokenConfigured bool   `json:"botTokenConfigured"`
	ChatID             string `json:"chatId"`
	TimeoutSec         int    `json:"timeoutSec"`
}

type LastTestView struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	At      string `json:"at"`
}

type View struct {
	SMTP                SMTPView     `json:"smtp"`
	Telegram            TelegramView `json:"telegram"`
	QuotaWarningPercent int          `json:"quotaWarningPercent"`
	ExpiryWarningHours  int          `json:"expiryWarningHours"`
	LastTest            LastTestView `json:"lastTest"`
}

type delivery struct {
	settings domain.NotificationSettings
	message  Message
}

type Service struct {
	repo     Repository
	smtp     SMTPSender
	telegram TelegramSender
	log      *slog.Logger
	queue    chan delivery
	now      func() time.Time
}

func New(repo Repository, smtp SMTPSender, telegram TelegramSender, log *slog.Logger) *Service {
	if smtp == nil {
		smtp = NewSMTPClient()
	}
	if telegram == nil {
		telegram = NewTelegramClient()
	}
	return &Service{repo: repo, smtp: smtp, telegram: telegram, log: log, queue: make(chan delivery, 64), now: time.Now}
}

func (s *Service) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case d := <-s.queue:
			s.deliver(ctx, d.settings, d.message)
		}
	}
}

func (s *Service) Get(ctx context.Context) (View, error) {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return View{}, err
	}
	return view(settings), nil
}

func (s *Service) Update(ctx context.Context, in Update) (View, error) {
	current, err := s.repo.GetSettings(ctx)
	if err != nil {
		return View{}, err
	}
	next := domain.NotificationSettings{
		SMTPEnabled: in.SMTP.Enabled, SMTPHost: strings.TrimSpace(in.SMTP.Host), SMTPPort: in.SMTP.Port,
		SMTPMode: strings.ToLower(strings.TrimSpace(in.SMTP.Mode)), SMTPFrom: strings.TrimSpace(in.SMTP.From),
		SMTPRecipients: cleanStrings(in.SMTP.Recipients), SMTPUsername: strings.TrimSpace(in.SMTP.Username),
		SMTPPassword: current.SMTPPassword, SMTPTimeoutSec: in.SMTP.TimeoutSec,
		TelegramEnabled: in.Telegram.Enabled, TelegramAPIBase: strings.TrimRight(strings.TrimSpace(in.Telegram.APIBase), "/"),
		TelegramBotToken: current.TelegramBotToken, TelegramChatID: strings.TrimSpace(in.Telegram.ChatID),
		TelegramTimeoutSec: in.Telegram.TimeoutSec, QuotaWarningPercent: in.QuotaWarningPercent,
		ExpiryWarningHours: in.ExpiryWarningHours,
	}
	if in.SMTP.ClearPassword {
		next.SMTPPassword = ""
	} else if in.SMTP.Password != "" {
		next.SMTPPassword = in.SMTP.Password
	}
	if in.Telegram.ClearBotToken {
		next.TelegramBotToken = ""
	} else if in.Telegram.BotToken != "" {
		next.TelegramBotToken = in.Telegram.BotToken
	}
	normalize(&next)
	if err := validate(next); err != nil {
		return View{}, err
	}
	if err := s.repo.SaveSettings(ctx, next); err != nil {
		return View{}, err
	}
	updated, err := s.repo.GetSettings(ctx)
	if err != nil {
		return View{}, err
	}
	return view(updated), nil
}

func (s *Service) Test(ctx context.Context, channel string) (LastTestView, error) {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return LastTestView{}, err
	}
	normalize(&settings)
	message := Message{Subject: "Shilka test notification", Body: "Notification channel is configured correctly."}
	channel = strings.ToLower(strings.TrimSpace(channel))
	switch channel {
	case "smtp":
		if err = validateSMTP(settings); err == nil {
			err = s.smtp.Send(ctx, settings, message)
		}
	case "telegram":
		if err = validateTelegram(settings); err == nil {
			err = s.telegram.Send(ctx, settings, message)
		}
	default:
		return LastTestView{}, fmt.Errorf("%w: channel must be smtp or telegram", ErrValidation)
	}
	now := s.now().UTC()
	result := LastTestView{Channel: channel, OK: err == nil, Message: "sent", At: now.Format(time.RFC3339)}
	if err != nil {
		result.Message = s.redact(settings, err.Error())
	}
	if saveErr := s.repo.SaveLastTest(ctx, channel, result.OK, result.Message, now); saveErr != nil {
		return result, saveErr
	}
	if err != nil {
		if errors.Is(err, ErrValidation) {
			return result, fmt.Errorf("%w: %s", ErrValidation, result.Message)
		}
		return result, errors.New(result.Message)
	}
	return result, nil
}

// ObserveClient emits quota and expiry transitions with persistent dedupe.
func (s *Service) ObserveClient(ctx context.Context, client domain.Client, now time.Time) error {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return err
	}
	normalize(&settings)
	subjectID := strconv.FormatInt(client.ID, 10)
	quotaLevel := 0
	if client.TotalQuota > 0 {
		usedPercent := int(float64(client.UsedTotal()) / float64(client.TotalQuota) * 100)
		if client.QuotaExceeded() {
			quotaLevel = domain.NotificationLevelTerminal
		} else if settings.QuotaWarningPercent > 0 && usedPercent >= settings.QuotaWarningPercent {
			quotaLevel = domain.NotificationLevelWarning
		}
	}
	quotaFingerprint := strconv.FormatInt(client.TotalQuota, 10)
	if err := s.observe(ctx, settings, "client", subjectID, domain.NotificationEventQuota, quotaFingerprint, quotaLevel,
		Message{Subject: "Shilka client quota", Body: clientMessage(client.Name, "traffic quota", quotaLevel)}); err != nil {
		return err
	}

	expiryLevel := 0
	expiryFingerprint := "none"
	if client.Expiry != nil {
		expiryFingerprint = client.Expiry.UTC().Format(time.RFC3339Nano)
		if client.IsExpired(now) {
			expiryLevel = domain.NotificationLevelTerminal
		} else if settings.ExpiryWarningHours > 0 && client.Expiry.Sub(now) <= time.Duration(settings.ExpiryWarningHours)*time.Hour {
			expiryLevel = domain.NotificationLevelWarning
		}
	}
	return s.observe(ctx, settings, "client", subjectID, domain.NotificationEventExpiry, expiryFingerprint, expiryLevel,
		Message{Subject: "Shilka client expiry", Body: clientMessage(client.Name, "expiry", expiryLevel)})
}

// NotifyFailure emits core/apply failures and suppresses identical errors for a short cooldown.
func (s *Service) NotifyFailure(ctx context.Context, eventType string, cause error) {
	if cause == nil {
		return
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		s.log.Warn("load notification settings", slog.String("error", err.Error()))
		return
	}
	if !settings.SMTPEnabled && !settings.TelegramEnabled {
		return
	}
	safe := s.redact(settings, cause.Error())
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(safe)))
	state, stateErr := s.repo.GetEventState(ctx, "core", "global", eventType)
	if stateErr == nil && state.Fingerprint == hash && s.now().Sub(state.LastSentAt) < coreErrorCooldown {
		return
	}
	if stateErr != nil && !errors.Is(stateErr, repo.ErrNotFound) {
		s.log.Warn("load notification state", slog.String("error", stateErr.Error()))
		return
	}
	if !s.enqueue(settings, Message{Subject: "Shilka core failure", Body: eventType + ": " + safe}) {
		return
	}
	if err := s.repo.SaveEventState(ctx, domain.NotificationEventState{SubjectType: "core", SubjectID: "global", EventType: eventType, Fingerprint: hash, Level: domain.NotificationLevelTerminal, LastSentAt: s.now().UTC()}); err != nil {
		s.log.Warn("save notification state", slog.String("error", err.Error()))
	}
}

func (s *Service) observe(ctx context.Context, settings domain.NotificationSettings, subjectType, subjectID, eventType, fingerprint string, level int, message Message) error {
	if level == 0 {
		_, err := s.repo.GetEventState(ctx, subjectType, subjectID, eventType)
		if errors.Is(err, repo.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return s.repo.DeleteEventState(ctx, subjectType, subjectID, eventType)
	}
	state, err := s.repo.GetEventState(ctx, subjectType, subjectID, eventType)
	if err == nil && state.Fingerprint == fingerprint && state.Level >= level {
		return nil
	}
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return err
	}
	if !s.enqueue(settings, message) {
		return nil
	}
	return s.repo.SaveEventState(ctx, domain.NotificationEventState{SubjectType: subjectType, SubjectID: subjectID, EventType: eventType, Fingerprint: fingerprint, Level: level, LastSentAt: s.now().UTC()})
}

func (s *Service) enqueue(settings domain.NotificationSettings, message Message) bool {
	if !settings.SMTPEnabled && !settings.TelegramEnabled {
		return false
	}
	select {
	case s.queue <- delivery{settings: settings, message: message}:
		return true
	default:
		s.log.Warn("notification queue full")
		return false
	}
}

func (s *Service) deliver(ctx context.Context, settings domain.NotificationSettings, message Message) {
	if settings.SMTPEnabled {
		if err := s.smtp.Send(ctx, settings, message); err != nil {
			s.log.Warn("smtp notification", slog.String("error", s.redact(settings, err.Error())))
		}
	}
	if settings.TelegramEnabled {
		if err := s.telegram.Send(ctx, settings, message); err != nil {
			s.log.Warn("telegram notification", slog.String("error", s.redact(settings, err.Error())))
		}
	}
}

func (s *Service) redact(settings domain.NotificationSettings, text string) string {
	authPlain := ""
	if settings.SMTPUsername != "" || settings.SMTPPassword != "" {
		authPlain = base64.StdEncoding.EncodeToString([]byte("\x00" + settings.SMTPUsername + "\x00" + settings.SMTPPassword))
	}
	return Redact(text, settings.SMTPPassword, settings.TelegramBotToken, authPlain)
}

// RedactError sanitizes errors before handlers, logs, or API responses expose them.
func (s *Service) RedactError(ctx context.Context, text string) string {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return Redact(text)
	}
	return s.redact(settings, text)
}

func view(s domain.NotificationSettings) View {
	at := ""
	if s.LastTestAt != nil {
		at = s.LastTestAt.UTC().Format(time.RFC3339)
	}
	return View{
		SMTP:                SMTPView{Enabled: s.SMTPEnabled, Host: s.SMTPHost, Port: s.SMTPPort, Mode: s.SMTPMode, From: s.SMTPFrom, Recipients: append([]string(nil), s.SMTPRecipients...), Username: s.SMTPUsername, PasswordConfigured: s.SMTPPassword != "", TimeoutSec: s.SMTPTimeoutSec},
		Telegram:            TelegramView{Enabled: s.TelegramEnabled, APIBase: s.TelegramAPIBase, BotTokenConfigured: s.TelegramBotToken != "", ChatID: s.TelegramChatID, TimeoutSec: s.TelegramTimeoutSec},
		QuotaWarningPercent: s.QuotaWarningPercent, ExpiryWarningHours: s.ExpiryWarningHours,
		LastTest: LastTestView{Channel: s.LastTestChannel, OK: s.LastTestOK, Message: s.LastTestMessage, At: at},
	}
}

func normalize(s *domain.NotificationSettings) {
	if s.SMTPPort == 0 {
		s.SMTPPort = 587
	}
	if s.SMTPMode == "" {
		s.SMTPMode = "starttls"
	}
	if s.SMTPTimeoutSec == 0 {
		s.SMTPTimeoutSec = 10
	}
	if s.TelegramAPIBase == "" {
		s.TelegramAPIBase = "https://api.telegram.org"
	}
	if s.TelegramTimeoutSec == 0 {
		s.TelegramTimeoutSec = 10
	}
}

func validate(s domain.NotificationSettings) error {
	if s.QuotaWarningPercent < 0 || s.QuotaWarningPercent > 100 {
		return fmt.Errorf("%w: quota warning must be between 0 and 100", ErrValidation)
	}
	if s.ExpiryWarningHours < 0 || s.ExpiryWarningHours > 8760 {
		return fmt.Errorf("%w: expiry warning must be between 0 and 8760 hours", ErrValidation)
	}
	if s.SMTPTimeoutSec < 1 || s.SMTPTimeoutSec > 60 || s.TelegramTimeoutSec < 1 || s.TelegramTimeoutSec > 60 {
		return fmt.Errorf("%w: provider timeout must be between 1 and 60 seconds", ErrValidation)
	}
	if s.SMTPEnabled {
		if err := validateSMTP(s); err != nil {
			return err
		}
	}
	if s.TelegramEnabled {
		if err := validateTelegram(s); err != nil {
			return err
		}
	}
	return nil
}

func validateSMTP(s domain.NotificationSettings) error {
	if s.SMTPHost == "" || s.SMTPPort < 1 || s.SMTPPort > 65535 {
		return fmt.Errorf("%w: SMTP host and port are required", ErrValidation)
	}
	if s.SMTPMode != "starttls" && s.SMTPMode != "tls" {
		return fmt.Errorf("%w: SMTP mode must be starttls or tls", ErrValidation)
	}
	if _, err := mail.ParseAddress(s.SMTPFrom); err != nil {
		return fmt.Errorf("%w: invalid SMTP sender", ErrValidation)
	}
	if len(s.SMTPRecipients) == 0 {
		return fmt.Errorf("%w: at least one SMTP recipient is required", ErrValidation)
	}
	for _, recipient := range s.SMTPRecipients {
		if _, err := mail.ParseAddress(recipient); err != nil {
			return fmt.Errorf("%w: invalid SMTP recipient", ErrValidation)
		}
	}
	return nil
}

func validateTelegram(s domain.NotificationSettings) error {
	if s.TelegramBotToken == "" || s.TelegramChatID == "" {
		return fmt.Errorf("%w: Telegram bot token and chat ID are required", ErrValidation)
	}
	u, err := url.Parse(s.TelegramAPIBase)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: invalid Telegram API base", ErrValidation)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopbackHost(u.Hostname())) {
		return fmt.Errorf("%w: Telegram API base must use HTTPS or loopback HTTP", ErrValidation)
	}
	return nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func clientMessage(name, metric string, level int) string {
	if level == domain.NotificationLevelTerminal {
		return fmt.Sprintf("Client %q reached terminal %s state.", name, metric)
	}
	return fmt.Sprintf("Client %q crossed the %s warning threshold.", name, metric)
}
