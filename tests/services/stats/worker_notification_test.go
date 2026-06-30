package stats_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/services/stats"
)

type workerLive struct{}

func (workerLive) Sample(context.Context) (stats.Live, error) { return stats.Live{}, nil }

type workerClients struct {
	mu       sync.Mutex
	client   domain.Client
	disabled int
}

func (c *workerClients) List(context.Context) ([]domain.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return []domain.Client{c.client}, nil
}
func (c *workerClients) AddTraffic(context.Context, []domain.TrafficDelta) error { return nil }
func (c *workerClients) SetStatus(context.Context, int64, domain.ClientStatus, bool) error {
	c.mu.Lock()
	c.disabled++
	c.client.Enabled = false
	c.mu.Unlock()
	return nil
}
func (c *workerClients) SetFirstUsed(context.Context, int64, any) error        { return nil }
func (c *workerClients) SetLastUsedAt(context.Context, int64, time.Time) error { return nil }

type workerRollup struct{}

func (workerRollup) AddDaily(context.Context, string, int64, int64) error { return nil }

type workerTrigger struct{}

func (workerTrigger) Trigger() {}

type workerObserver struct {
	mu    sync.Mutex
	count int
}

func (o *workerObserver) ObserveClient(context.Context, domain.Client, time.Time) error {
	o.mu.Lock()
	o.count++
	o.mu.Unlock()
	return nil
}
func (o *workerObserver) seen() int { o.mu.Lock(); defer o.mu.Unlock(); return o.count }

func TestWorkerObservesClientBeforeTerminalDisable(t *testing.T) {
	clients := &workerClients{client: domain.Client{ID: 1, Name: "alice", Enabled: true, Status: domain.ClientStatusActive, TotalQuota: 100, UsedDown: 100}}
	observer := &workerObserver{}
	worker := stats.NewWorker(workerLive{}, nil, nil, clients, workerRollup{}, workerTrigger{}, &stats.LiveHolder{}, stats.WorkerConfig{SampleInterval: time.Hour, EnforceInterval: 5 * time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)), observer)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Run(ctx)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		clients.mu.Lock()
		disabled := clients.disabled
		clients.mu.Unlock()
		if observer.seen() > 0 && disabled > 0 {
			cancel()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	t.Fatalf("observer=%d terminal disable missing", observer.seen())
}
