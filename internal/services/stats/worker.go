package stats

import (
	"context"
	"log/slog"
	"time"

	"sing-box-web-panel/internal/domain"
)

// ClientStore is the subset of the client repository the worker needs.
type ClientStore interface {
	List(ctx context.Context) ([]domain.Client, error)
	AddTraffic(ctx context.Context, deltas []domain.TrafficDelta) error
	SetStatus(ctx context.Context, id int64, status domain.ClientStatus, enabled bool) error
	SetFirstUsed(ctx context.Context, id int64, at any) error
	SetLastUsedAt(ctx context.Context, id int64, at time.Time) error
}

// RollupStore persists the daily traffic rollup.
type RollupStore interface {
	AddDaily(ctx context.Context, day string, up, down int64) error
}

// ConfigTrigger requests a (debounced) config apply after enforcement changes.
type ConfigTrigger interface {
	Trigger()
}

type ClientObserver interface {
	ObserveClient(ctx context.Context, client domain.Client, now time.Time) error
}

// UserHeartbeat reports the last time each tracked user produced any activity.
// Worker mirrors those timestamps onto clients.last_used_at so the 5-min
// online threshold in the handler decays naturally once the client goes
// quiet.
type UserHeartbeat interface {
	UserSeen() map[string]time.Time
}

// WorkerConfig holds the worker's polling cadences.
type WorkerConfig struct {
	SampleInterval  time.Duration // live dashboard metrics
	EnforceInterval time.Duration // expiry/quota checks
	FlushInterval   time.Duration // per-user accounting batch (UserSource only)
}

// Worker polls the core for metrics and enforces client limits.
type Worker struct {
	live      LiveSource
	users     UserSource    // optional; nil disables per-user traffic accounting
	heartbeat UserHeartbeat // optional; nil disables log-driven heartbeat
	clients   ClientStore
	rollup    RollupStore
	trigger   ConfigTrigger
	observer  ClientObserver
	holder    *LiveHolder
	cfg       WorkerConfig
	log       *slog.Logger
}

func NewWorker(live LiveSource, users UserSource, heartbeat UserHeartbeat, clients ClientStore, rollup RollupStore, trigger ConfigTrigger, holder *LiveHolder, cfg WorkerConfig, log *slog.Logger, observers ...ClientObserver) *Worker {
	if cfg.SampleInterval <= 0 {
		cfg.SampleInterval = 2 * time.Second
	}
	if cfg.EnforceInterval <= 0 {
		cfg.EnforceInterval = 30 * time.Second
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	worker := &Worker{
		live: live, users: users, heartbeat: heartbeat,
		clients: clients, rollup: rollup,
		trigger: trigger, holder: holder, cfg: cfg, log: log,
	}
	if len(observers) > 0 {
		worker.observer = observers[0]
	}
	return worker
}

// Run launches the worker loops and returns when ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	go w.liveLoop(ctx)
	go w.enforceLoop(ctx)
	if w.users != nil || w.heartbeat != nil {
		go w.accountingLoop(ctx)
	}
}

func (w *Worker) liveLoop(ctx context.Context) {
	t := time.NewTicker(w.cfg.SampleInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			live, err := w.live.Sample(ctx)
			if err != nil {
				w.log.Debug("live sample", slog.String("error", err.Error()))
				continue
			}
			w.holder.Set(live)
		}
	}
}

func (w *Worker) enforceLoop(ctx context.Context) {
	t := time.NewTicker(w.cfg.EnforceInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.enforce(ctx)
		}
	}
}

// enforce disables clients that have expired or exhausted their quota, then
// triggers a config apply if anything changed.
func (w *Worker) enforce(ctx context.Context) {
	list, err := w.clients.List(ctx)
	if err != nil {
		w.log.Debug("enforce list clients", slog.String("error", err.Error()))
		return
	}
	now := time.Now()
	changed := false
	for i := range list {
		c := list[i]
		if !c.Enabled || c.Status != domain.ClientStatusActive {
			continue
		}
		if w.observer != nil {
			if err := w.observer.ObserveClient(ctx, c, now); err != nil {
				w.log.Warn("observe client notification", slog.Int64("id", c.ID), slog.String("error", err.Error()))
			}
		}
		if c.IsExpired(now) || c.QuotaExceeded() {
			if err := w.clients.SetStatus(ctx, c.ID, domain.ClientStatusExpired, false); err != nil {
				w.log.Warn("disable client", slog.Int64("id", c.ID), slog.String("error", err.Error()))
				continue
			}
			changed = true
		}
	}
	if changed && w.trigger != nil {
		w.trigger.Trigger()
	}
}

func (w *Worker) accountingLoop(ctx context.Context) {
	t := time.NewTicker(w.cfg.FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.account(ctx)
		}
	}
}

// account pulls per-user deltas from the UserSource, batches them, refreshes
// the heartbeat for active users that produced no measurable traffic this
// tick, updates the daily rollup, then runs enforcement.
func (w *Worker) account(ctx context.Context) {
	var deltas []domain.UserTraffic
	if w.users != nil {
		var err error
		deltas, err = w.users.UserDeltas(ctx)
		if err != nil {
			w.log.Debug("user deltas", slog.String("error", err.Error()))
			deltas = nil
		}
	}

	var heartbeat map[string]time.Time
	if w.heartbeat != nil {
		heartbeat = w.heartbeat.UserSeen()
	}

	if len(deltas) == 0 && len(heartbeat) == 0 {
		return
	}

	list, err := w.clients.List(ctx)
	if err != nil {
		return
	}
	byName := make(map[string]domain.Client, len(list))
	for _, c := range list {
		byName[c.Name] = c
	}

	var (
		batch          []domain.TrafficDelta
		dayUp, dayDown int64
		now            = time.Now()
		bumped         = make(map[int64]struct{}, len(deltas)+len(heartbeat))
	)
	for _, ut := range deltas {
		c, ok := byName[ut.Name]
		if !ok || (ut.Up == 0 && ut.Down == 0) {
			continue
		}
		batch = append(batch, domain.TrafficDelta{ClientID: c.ID, Up: ut.Up, Down: ut.Down})
		dayUp += ut.Up
		dayDown += ut.Down
		bumped[c.ID] = struct{}{}
		if c.StartAfterFirstUse && c.FirstUsedAt == nil {
			if err := w.clients.SetFirstUsed(ctx, c.ID, now); err != nil {
				w.log.Debug("set first used", slog.Int64("id", c.ID), slog.String("error", err.Error()))
			}
		}
	}

	if len(batch) > 0 {
		if err := w.clients.AddTraffic(ctx, batch); err != nil {
			w.log.Warn("add traffic", slog.String("error", err.Error()))
			return
		}
		if err := w.rollup.AddDaily(ctx, now.UTC().Format("2006-01-02"), dayUp, dayDown); err != nil {
			w.log.Debug("add daily rollup", slog.String("error", err.Error()))
		}
	}

	// Heartbeat refresh: set last_used_at to the user's last observed log
	// timestamp (not now) so it freezes the moment the client goes quiet.
	for name, seenAt := range heartbeat {
		c, ok := byName[name]
		if !ok {
			continue
		}
		if _, already := bumped[c.ID]; already {
			continue
		}
		if err := w.clients.SetLastUsedAt(ctx, c.ID, seenAt); err != nil {
			w.log.Debug("set last used", slog.Int64("id", c.ID), slog.String("error", err.Error()))
		}
	}

	w.enforce(ctx)
}
