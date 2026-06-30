package singbox_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/services/singbox"
)

type noopProcessManager struct{}

func (noopProcessManager) Start(context.Context) error   { return nil }
func (noopProcessManager) Stop(context.Context) error    { return nil }
func (noopProcessManager) Restart(context.Context) error { return nil }
func (noopProcessManager) Reload(context.Context) error  { return nil }
func (noopProcessManager) Status(context.Context) (singbox.Status, error) {
	return singbox.Status{}, nil
}

type applyNotifier struct{ events []string }

func (n *applyNotifier) NotifyFailure(_ context.Context, eventType string, _ error) {
	n.events = append(n.events, eventType)
}

func TestApplierCheckFailureNotifies(t *testing.T) {
	gen := singbox.NewGenerator(fakeInbounds{}, fakeClients{}, singbox.GeneratorConfig{ClashAPIAddress: "127.0.0.1:9090"})
	checker := singbox.NewChecker(filepath.Join(t.TempDir(), "missing-sing-box"), time.Second)
	notifier := &applyNotifier{}
	applier := singbox.NewApplier(gen, checker, noopProcessManager{}, nil, filepath.Join(t.TempDir(), "config.json"), slog.New(slog.NewTextHandler(io.Discard, nil)), notifier)
	if err := applier.Apply(context.Background()); err == nil {
		t.Fatal("expected check failure")
	}
	if len(notifier.events) != 1 || notifier.events[0] != domain.NotificationEventCheck {
		t.Fatalf("events=%v", notifier.events)
	}
}
