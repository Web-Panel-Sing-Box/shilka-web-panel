package singbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"sing-box-web-panel/internal/domain"
)

// RevisionRecorder persists the outcome of each apply attempt.
type RevisionRecorder interface {
	Create(ctx context.Context, rev *domain.ConfigRevision) error
}

type FailureNotifier interface {
	NotifyFailure(ctx context.Context, eventType string, cause error)
}

type ErrorRedactor interface {
	RedactError(ctx context.Context, text string) string
}

const maxConfigBackups = 5

// Applier renders, validates and applies the live sing-box config. It also
// implements the ConfigTrigger interface consumed by the inbound/client
// services: Trigger() schedules a debounced apply so bulk edits coalesce.
type Applier struct {
	gen        *Generator
	checker    *Checker
	pm         ProcessManager
	revs       RevisionRecorder
	configPath string
	log        *slog.Logger
	notifier   FailureNotifier

	mu sync.Mutex // serializes Apply

	triggerCh chan struct{}
	debounce  time.Duration
}

func NewApplier(gen *Generator, checker *Checker, pm ProcessManager, revs RevisionRecorder, configPath string, log *slog.Logger, notifiers ...FailureNotifier) *Applier {
	a := &Applier{
		gen:        gen,
		checker:    checker,
		pm:         pm,
		revs:       revs,
		configPath: configPath,
		log:        log,
		triggerCh:  make(chan struct{}, 1),
		debounce:   800 * time.Millisecond,
	}
	if len(notifiers) > 0 {
		a.notifier = notifiers[0]
	}
	return a
}

// Preview renders the config without validating or applying it.
func (a *Applier) Preview(ctx context.Context) ([]byte, error) {
	return a.gen.Render(ctx)
}

// Trigger schedules a debounced apply. Safe to call from any goroutine; extra
// triggers while one is pending are coalesced.
func (a *Applier) Trigger() {
	select {
	case a.triggerCh <- struct{}{}:
	default:
	}
}

// ApplyIfMissing generates and installs the config only if no config file
// exists at the configured path yet. Used by the Start handler to bootstrap
// the first config before launching the core.
func (a *Applier) ApplyIfMissing(ctx context.Context) error {
	if _, err := os.Stat(a.configPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return a.failure(ctx, domain.NotificationEventApply, fmt.Errorf("check config path: %w", err))
	}
	a.log.Info("initial config not found, applying first config")
	return a.Apply(ctx)
}

// Run consumes trigger events and applies after a quiet period. It returns when
// ctx is cancelled.
func (a *Applier) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.triggerCh:
			if !a.waitQuiet(ctx) {
				return
			}
			if err := a.Apply(ctx); err != nil {
				a.log.Error("apply config", slog.String("error", a.safeError(ctx, err.Error())))
			}
		}
	}
}

// waitQuiet blocks until no trigger arrives for the debounce window. Returns
// false if ctx is cancelled.
func (a *Applier) waitQuiet(ctx context.Context) bool {
	t := time.NewTimer(a.debounce)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-a.triggerCh:
			if !t.Stop() {
				<-t.C
			}
			t.Reset(a.debounce)
		case <-t.C:
			return true
		}
	}
}

// Apply renders, validates and (on success) installs the config, then reloads a
// running core. A failed check leaves the live config untouched and records the
// failure. It returns the check/render error so callers (e.g. the reload
// endpoint) can surface it to the UI.
func (a *Applier) Apply(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	data, err := a.gen.Render(ctx)
	if err != nil {
		return a.failure(ctx, domain.NotificationEventApply, err)
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])

	if err := os.MkdirAll(filepath.Dir(a.configPath), 0o750); err != nil {
		return a.failure(ctx, domain.NotificationEventApply, fmt.Errorf("create config dir: %w", err))
	}

	tmp := a.configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return a.failure(ctx, domain.NotificationEventApply, fmt.Errorf("write temp config: %w", err))
	}

	if err := a.checker.Check(ctx, tmp); err != nil {
		os.Remove(tmp)
		a.record(ctx, sha, false, a.safeError(ctx, err.Error()))
		return a.failure(ctx, domain.NotificationEventCheck, err)
	}

	a.backupCurrent()
	if err := os.Rename(tmp, a.configPath); err != nil {
		a.record(ctx, sha, false, a.safeError(ctx, err.Error()))
		return a.failure(ctx, domain.NotificationEventApply, fmt.Errorf("install config: %w", err))
	}

	// Reload only if the core is already running; otherwise the freshly written
	// config will be picked up on the next explicit Start.
	if st, err := a.pm.Status(ctx); err == nil && st.Running {
		if err := a.pm.Reload(ctx); err != nil {
			a.log.Warn("reload after apply failed", slog.String("error", a.safeError(ctx, err.Error())))
			if a.notifier != nil {
				a.notifier.NotifyFailure(ctx, domain.NotificationEventReload, err)
			}
		}
	}

	a.record(ctx, sha, true, "")
	a.log.Info("config applied", slog.String("sha256", sha[:12]))
	return nil
}

func (a *Applier) failure(ctx context.Context, eventType string, err error) error {
	if a.notifier != nil {
		a.notifier.NotifyFailure(ctx, eventType, err)
	}
	return err
}

func (a *Applier) safeError(ctx context.Context, text string) string {
	if redactor, ok := a.notifier.(ErrorRedactor); ok {
		return redactor.RedactError(ctx, text)
	}
	return text
}

func (a *Applier) record(ctx context.Context, sha string, ok bool, errMsg string) {
	if a.revs == nil {
		return
	}
	if err := a.revs.Create(ctx, &domain.ConfigRevision{SHA256: sha, OK: ok, Error: errMsg}); err != nil {
		a.log.Warn("record config revision", slog.String("error", err.Error()))
	}
}

// backupCurrent copies the live config aside before overwrite and prunes old
// backups, keeping the most recent maxConfigBackups.
func (a *Applier) backupCurrent() {
	if _, err := os.Stat(a.configPath); err != nil {
		return
	}
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		return
	}
	backup := fmt.Sprintf("%s.bak-%d", a.configPath, time.Now().Unix())
	if err := os.WriteFile(backup, data, 0o640); err != nil {
		return
	}

	matches, _ := filepath.Glob(a.configPath + ".bak-*")
	if len(matches) > maxConfigBackups {
		sort.Strings(matches)
		for _, old := range matches[:len(matches)-maxConfigBackups] {
			os.Remove(old)
		}
	}
}
