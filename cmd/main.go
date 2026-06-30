package main

//	@title			Shilka API
//	@version		0.1.0
//	@description	Local-first web panel for managing a sing-box process.
//	@host			localhost:8080
//	@BasePath		/api

//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Enter "Bearer " followed by the JWT token.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"sing-box-web-panel/docs"
	"sing-box-web-panel/internal/config"
	"sing-box-web-panel/internal/domain"
	libauth "sing-box-web-panel/internal/lib/auth"
	"sing-box-web-panel/internal/lib/sl"
	sqliterepo "sing-box-web-panel/internal/repo/sqlite"
	svcapitoken "sing-box-web-panel/internal/services/apitoken"
	"sing-box-web-panel/internal/services/auth"
	svcclient "sing-box-web-panel/internal/services/client"
	svcinbound "sing-box-web-panel/internal/services/inbound"
	"sing-box-web-panel/internal/services/logbuf"
	svcnode "sing-box-web-panel/internal/services/node"
	"sing-box-web-panel/internal/services/notification"
	"sing-box-web-panel/internal/services/scheduler"
	svcsettings "sing-box-web-panel/internal/services/settings"
	"sing-box-web-panel/internal/services/singbox"
	"sing-box-web-panel/internal/services/stats"
	"sing-box-web-panel/internal/services/sysstat"
	"sing-box-web-panel/internal/services/tlsmgr"
	"sing-box-web-panel/internal/services/updater"
	"sing-box-web-panel/internal/transport/handler"
	"sing-box-web-panel/internal/transport/middleware"
	"sing-box-web-panel/internal/version"

	httpSwagger "github.com/swaggo/http-swagger/v2"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] != "run" {
		if err := runCLI(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "run" {
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	}
	runServer()
}

func runServer() {
	cfg := config.MustLoad()
	logBuf := logbuf.New(cfg.Logging.MaxMemoryLines)
	log := setupLogger(cfg.Env, cfg.Logging, logBuf)

	log.Info("starting server", slog.String("env", cfg.Env))

	storage, err := sqliterepo.New(cfg.Database, log)
	if err != nil {
		log.Error("failed to connect to database", sl.Error(err))
		os.Exit(1)
	}

	hasher := libauth.NewArgon2Hasher(
		cfg.Auth.Argon2MemoryKB,
		cfg.Auth.Argon2Iterations,
		cfg.Auth.Argon2Parallelism,
	)
	jwtMgr := libauth.NewJWTManager(cfg.Auth.JWTSecret, cfg.Auth.JWTExpiry)
	totpMgr := libauth.NewTOTPManager("Shilka")

	adminRepo := sqliterepo.NewAdminRepo(storage)
	recoveryRepo := sqliterepo.NewRecoveryRepo(storage)

	totpAdapter := auth.NewTOTPAdapter(totpMgr)

	authSvc := auth.NewService(
		adminRepo,
		recoveryRepo,
		hasher,
		jwtMgr,
		totpAdapter,
		libauth.GenerateRecoveryCode,
	)

	if err := authSvc.SeedAdmin(context.Background(), cfg.Auth.AdminUser, cfg.Auth.AdminPassword); err != nil {
		log.Error("bootstrap admin", sl.Error(err))
		os.Exit(1)
	}

	debug.SetMemoryLimit(mustBytes(cfg.Runtime.GoMemLimit))
	debug.SetGCPercent(cfg.Runtime.GoGC)

	mux := http.NewServeMux()

	docs.SwaggerInfo.BasePath = "/api"
	docs.SwaggerInfo.Version = version.Panel()
	mux.Handle("GET /swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))

	authHandler := handler.NewAuthHandler(authSvc, log)
	authHandler.Register(mux)

	healthHandler := handler.NewHealthHandler()
	healthHandler.Register(mux)

	// Background worker context: cancelled during shutdown to stop the apply loop.
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	inboundRepo := sqliterepo.NewInboundRepo(storage)
	clientRepo := sqliterepo.NewClientRepo(storage)
	apiTokenRepo := sqliterepo.NewAPITokenRepo(storage)
	nodeRepo := sqliterepo.NewNodeRepo(storage)
	configRevRepo := sqliterepo.NewConfigRevisionRepo(storage)
	settingRepo := sqliterepo.NewSettingRepo(storage)
	notificationRepo := sqliterepo.NewNotificationRepo(storage)
	notificationSvc := notification.New(notificationRepo, nil, nil, log)
	go notificationSvc.Run(rootCtx)

	// Seed defaults for known settings so the panel always has sane values.
	seedSetting(context.Background(), settingRepo, domain.SettingPanelName, "Shilka")
	seedSetting(context.Background(), settingRepo, domain.SettingLogLevel, "info")

	// Read DB-backed settings that affect config generation.
	logLevel := getSetting(context.Background(), settingRepo, domain.SettingLogLevel, "info")

	// Resolve sing-box paths to absolute so the subprocess working dir does not
	// double-apply to a relative config path.
	absConfigPath := absPath(cfg.SingBox.ConfigPath)
	absWorkingDir := absPath(cfg.SingBox.WorkingDir)

	absCoreLogPath := ""
	if cfg.SingBox.CoreLogPath != "" {
		absCoreLogPath = absPath(cfg.SingBox.CoreLogPath)
	}
	if absCoreLogPath != "" {
		go logBuf.TailFile(rootCtx, absCoreLogPath, logbuf.SourceCore, time.Second, log)
	}

	// Default certificate source for tls-mode inbounds (hysteria2/naive always
	// require tls) that have no ACME/cert configured: a panel-managed self-signed
	// keypair. Without it the generator emits empty certificate_path and sing-box
	// fails to bring up TLS (SIN-52). Non-fatal: on error the paths stay empty and
	// behaviour falls back to the previous (degraded) state.
	inboundTLSCert, inboundTLSKey, err := tlsmgr.EnsureSelfSigned(absPath(cfg.TLS.SelfSignedDir), cfg.TLS.SelfSignedHosts)
	if err != nil {
		log.Warn("ensure inbound self-signed cert", sl.Error(err))
	}

	// sing-box config generation + lifecycle.
	generator := singbox.NewGenerator(inboundRepo, clientRepo, singbox.GeneratorConfig{
		LogLevel:           logLevel,
		InboundListen:      "::",
		ClashAPIAddress:    cfg.SingBox.APIAddress,
		ClashAPISecret:     cfg.SingBox.APISecret,
		CacheFilePath:      filepath.Join(absWorkingDir, "cache.db"),
		StatsSource:        cfg.Stats.Source,
		V2RayAPIListen:     cfg.Stats.V2RayAPIAddress,
		CoreLogPath:        absCoreLogPath,
		DefaultTLSCertPath: inboundTLSCert,
		DefaultTLSKeyPath:  inboundTLSKey,
		Settings:           settingRepo,
	})
	checker := singbox.NewChecker(cfg.SingBox.BinaryPath, cfg.SingBox.CheckTimeout)
	processMgr := singbox.NewProcessManager(singbox.ProcessConfig{
		Mode:         cfg.SingBox.ProcessMode,
		Binary:       cfg.SingBox.BinaryPath,
		ConfigPath:   absConfigPath,
		WorkingDir:   absWorkingDir,
		ServiceName:  cfg.SingBox.ServiceName,
		RestartDelay: cfg.SingBox.RestartDelay,
	}, logBuf.Writer(), log)
	applier := singbox.NewApplier(generator, checker, processMgr, configRevRepo, absConfigPath, log, notificationSvc)
	go applier.Run(rootCtx)

	log.Debug("starting sing-box core")
	// Bootstrap the initial config and start the core automatically.
	if err := bootCore(context.Background(), applier, processMgr, log); err != nil {
		log.Warn("boot core", sl.Error(err))
		notificationSvc.NotifyFailure(context.Background(), domain.NotificationEventStart, err)
	}

	// Inbound and client management; the applier is the (debounced) ConfigTrigger.
	inboundSvc := svcinbound.NewService(inboundRepo, clientRepo, applier)
	clientSvc := svcclient.NewService(clientRepo, inboundRepo, applier)
	apiTokenSvc := svcapitoken.NewService(apiTokenRepo)
	nodeSvc := svcnode.NewService(nodeRepo, inboundRepo, clientRepo, svcnode.NewHTTPClient())
	go nodeSvc.Run(rootCtx, 10*time.Second, 15*time.Second)

	settingSvc := svcsettings.New(settingRepo, applier)
	taskRepo := sqliterepo.NewScheduledTaskRepo(storage)

	schedulerSvc := scheduler.New(taskRepo, log)
	schedulerSvc.RegisterAction(domain.ActionResetTrafficAll, func(ctx context.Context, params json.RawMessage) error {
		clients, err := clientRepo.List(ctx)
		if err != nil {
			return err
		}
		for _, c := range clients {
			if err := clientRepo.ResetTraffic(ctx, c.ID); err != nil {
				log.Warn("reset traffic", slog.Int64("client", c.ID), slog.String("error", err.Error()))
			}
		}
		log.Info("scheduled: reset all traffic", slog.Int("clients", len(clients)))
		return nil
	})
	schedulerSvc.RegisterAction(domain.ActionDeleteExpired, func(ctx context.Context, params json.RawMessage) error {
		clients, err := clientRepo.List(ctx)
		if err != nil {
			return err
		}
		now := time.Now()
		deleted := 0
		for _, c := range clients {
			if c.IsExpired(now) {
				if err := clientRepo.Delete(ctx, c.ID); err != nil {
					log.Warn("delete expired", slog.Int64("client", c.ID), slog.String("error", err.Error()))
					continue
				}
				deleted++
			}
		}
		log.Info("scheduled: delete expired clients", slog.Int("deleted", deleted))
		return nil
	})
	schedulerSvc.RegisterAction(domain.ActionBackupDB, func(ctx context.Context, params json.RawMessage) error {
		backupPath := cfg.Database.Path + ".backup"
		src, err := os.ReadFile(cfg.Database.Path)
		if err != nil {
			return fmt.Errorf("read db: %w", err)
		}
		if err := os.WriteFile(backupPath, src, 0o600); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
		log.Info("scheduled: database backup", slog.String("path", backupPath))
		return nil
	})
	schedulerSvc.RegisterAction(domain.ActionRotateRealityKeys, func(ctx context.Context, params json.RawMessage) error {
		log.Info("scheduled: rotate reality keys is not implemented yet")
		return nil
	})
	if err := schedulerSvc.Start(); err != nil {
		log.Error("start scheduler", sl.Error(err))
	}

	trafficRepo := sqliterepo.NewTrafficRepo(storage)
	sysReader := sysstat.New()

	// Traffic stats + quota worker. Clash REST is the live dashboard source. The
	// per-user source is V2Ray gRPC when the core was built with `with_v2ray_api`;
	// otherwise we attribute clash /connections entries to clients via the
	// per-user outbound tag the generator stamps on every active client
	// (chains[0] == "user-{id}").
	liveHolder := &stats.LiveHolder{}
	clashSource := stats.NewClashSource(cfg.SingBox.APIAddress, cfg.SingBox.APISecret)

	nameByID := func(id int64) string {
		c, err := clientRepo.GetByID(rootCtx, id)
		if err != nil || c == nil {
			return ""
		}
		return c.Name
	}

	var (
		userSource stats.UserSource
		heartbeat  stats.UserHeartbeat
	)
	if cfg.Stats.Source == "v2ray" {
		userSource = stats.NewV2RaySource(cfg.Stats.V2RayAPIAddress)
		log.Info("per-user stats via v2ray api", slog.String("addr", cfg.Stats.V2RayAPIAddress))
	} else {
		clashUserSource := stats.NewClashUserSource(cfg.SingBox.APIAddress, cfg.SingBox.APISecret, nameByID)
		userSource = clashUserSource
		heartbeat = clashUserSource
		log.Info("per-user stats via clash api (chains-based attribution)")
	}
	statsWorker := stats.NewWorker(clashSource, userSource, heartbeat, clientRepo, trafficRepo, applier, liveHolder, stats.WorkerConfig{
		SampleInterval: cfg.Metrics.TrafficInterval,
		FlushInterval:  cfg.Metrics.BatchFlushInterval,
	}, log, notificationSvc)
	statsWorker.Run(rootCtx)

	handler.NewInboundHandler(inboundSvc, log, nodeSvc).Register(mux)
	handler.NewClientHandler(clientSvc, cfg.Sub.PublicURL, log, nodeSvc).Register(mux)
	handler.NewAPITokenHandler(apiTokenSvc, log).Register(mux)
	handler.NewCoreHandler(processMgr, applier, log, absCoreLogPath, notificationSvc).Register(mux)
	handler.NewNodeHandler(nodeSvc, inboundSvc, clientSvc, sysReader, processMgr, log).Register(mux)
	handler.NewSubscriptionHandler(clientRepo, inboundRepo, settingRepo, cfg.Sub.PublicURL, "", log).Register(mux)
	handler.NewDashboardHandler(sysReader, liveHolder, clientRepo, inboundRepo, trafficRepo, processMgr, log).Register(mux)
	handler.NewLogsHandler(logBuf).Register(mux)
	handler.NewSettingsHandler(settingSvc, log).Register(mux)
	handler.NewNotificationHandler(notificationSvc, log).Register(mux)
	handler.NewSchedulerHandler(schedulerSvc, log).Register(mux)
	updateSvc := updater.New(updater.Config{
		Repo:           cfg.Updates.Repo,
		ScriptPath:     cfg.Updates.ScriptPath,
		CurrentVersion: version.Panel(),
		CheckCacheTTL:  cfg.Updates.CheckCacheTTL,
		CommandTimeout: cfg.Updates.CommandTimeout,
	}, nil, nil, log)
	handler.NewPanelHandler(updateSvc, log).Register(mux)

	frontendHandler := handler.NewFrontendHandler(cfg.Frontend.ServeMode, cfg.Frontend.DiskPath, cfg.Frontend.CacheTTL, frontendDist)
	mux.Handle("/", frontendHandler)

	corsOrigins := []string{"http://localhost:3000", "http://127.0.0.1:3000"}

	stack := middleware.Auth(jwtMgr, log, apiTokenSvc)(mux)
	stack = middleware.CORS(corsOrigins)(stack)
	// Stricter brute-force limit on login, then a general per-IP API limit.
	stack = middleware.RateLimit(cfg.Auth.LoginRateLimit, middleware.LoginPathMatcher, log)(stack)
	stack = middleware.RateLimit(cfg.Auth.APIRateLimit, middleware.APIPathMatcher, log)(stack)
	stack = middleware.Logger(log)(stack)

	var httpHandler http.Handler = stack
	if cfg.HTTP.BasePath != "" {
		base := cfg.HTTP.BasePath
		httpHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == base {
				http.Redirect(w, r, base+"/", http.StatusMovedPermanently)
				return
			}
			http.StripPrefix(base, stack).ServeHTTP(w, r)
		})
	}

	server := &http.Server{
		Addr:           cfg.HTTP.Address,
		Handler:        httpHandler,
		ReadTimeout:    cfg.HTTP.ReadTimeout,
		WriteTimeout:   cfg.HTTP.WriteTimeout,
		IdleTimeout:    cfg.HTTP.IdleTimeout,
		MaxHeaderBytes: cfg.HTTP.MaxHeaderBytes,
	}

	tlsMgr := tlsmgr.New(tlsmgr.Config{
		Mode:            cfg.TLS.Mode,
		CertFile:        cfg.TLS.CertFile,
		KeyFile:         cfg.TLS.KeyFile,
		ACMEEmail:       cfg.TLS.ACMEEmail,
		ACMEDomains:     cfg.TLS.ACMEDomains,
		ACMECacheDir:    cfg.TLS.ACMECacheDir,
		SelfSignedHosts: cfg.TLS.SelfSignedHosts,
		SelfSignedDir:   cfg.TLS.SelfSignedDir,
	})
	tlsConf, err := tlsMgr.TLSConfig()
	if err != nil {
		log.Error("tls setup", sl.Error(err))
		os.Exit(1)
	}
	server.TLSConfig = tlsConf

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		scheme := "http"
		if tlsMgr.Enabled() {
			scheme = "https"
		}
		log.Info("server listening", slog.String("addr", cfg.HTTP.Address), slog.String("scheme", scheme))

		var serveErr error
		if tlsMgr.Enabled() {
			serveErr = server.ListenAndServeTLS("", "") // certs come from TLSConfig
		} else {
			serveErr = server.ListenAndServe()
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			log.Error("http server error", sl.Error(serveErr))
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	rootCancel() // stop background workers (apply loop, etc.)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown error", sl.Error(err))
	}

	if err := processMgr.Stop(context.Background()); err != nil {
		log.Warn("stop core", sl.Error(err))
	}

	<-schedulerSvc.Stop().Done()

	shutdown(storage, log, cfg)
	log.Info("stopped")
}

func shutdown(storage interface{ Close() error }, log *slog.Logger, _ *config.Config) {
	if err := storage.Close(); err != nil {
		log.Error("failed to close database", sl.Error(err))
	} else {
		log.Info("database connection closed")
	}
}

func bootCore(ctx context.Context, applier *singbox.Applier, pm singbox.ProcessManager, log *slog.Logger) error {
	// Always regenerate on boot so a panel upgrade that changes the schema
	// (new outbounds, new route rules, etc.) takes effect without a manual
	// nudge. Applier is idempotent: same hash → no-op restart.
	if err := applier.Apply(ctx); err != nil {
		return fmt.Errorf("apply initial config: %w", err)
	}
	if err := pm.Start(ctx); err != nil {
		return fmt.Errorf("start core: %w", err)
	}
	log.Info("core started")
	return nil
}

// absPath resolves p to an absolute path, falling back to p on error.
func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func setupLogger(env string, cfg config.LoggingConfig, buf *logbuf.Buffer) *slog.Logger {
	level := slogLevel(cfg.Level)
	out := io.Writer(os.Stdout)
	if cfg.FilePath != "" {
		fileOut, err := newRotateWriter(cfg.FilePath, int64(cfg.MaxFileSizeMB)*1024*1024, cfg.MaxFileBackups)
		if err == nil {
			out = io.MultiWriter(os.Stdout, fileOut)
		}
	}
	var base slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "text":
		base = slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})
	case "pretty":
		base = sl.PrettyHandlerOptions{SlogOpts: &slog.HandlerOptions{Level: level}}.NewPrettyHandler(out)
	default:
		if env == "dev" || env == "local" {
			base = sl.PrettyHandlerOptions{SlogOpts: &slog.HandlerOptions{Level: level}}.NewPrettyHandler(out)
		} else {
			base = slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level})
		}
	}
	return slog.New(logbuf.NewTeeHandler(base, buf))
}

func slogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type rotateWriter struct {
	path    string
	max     int64
	backups int
	mu      sync.Mutex
	file    *os.File
}

func newRotateWriter(path string, maxBytes int64, backups int) (*rotateWriter, error) {
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}
	if backups < 0 {
		backups = 0
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	return &rotateWriter{path: path, max: maxBytes, backups: backups, file: f}, nil
}

func (w *rotateWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateIfNeeded(int64(len(p))); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *rotateWriter) rotateIfNeeded(next int64) error {
	if w.file == nil {
		return nil
	}
	st, err := w.file.Stat()
	if err != nil {
		return err
	}
	if st.Size()+next <= w.max {
		return nil
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	if w.backups > 0 {
		for i := w.backups - 1; i >= 1; i-- {
			_ = os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1))
		}
		_ = os.Rename(w.path, w.path+".1")
	} else {
		_ = os.Remove(w.path)
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	w.file = f
	return nil
}

func mustBytes(s string) int64 {
	if s == "" {
		return 0
	}
	var n float64
	unit := "B"
	if _, err := fmt.Sscanf(s, "%f%s", &n, &unit); err != nil {
		return 0
	}
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "B":
		return int64(n)
	case "KB", "KIB":
		return int64(n * 1024)
	case "MB", "MIB":
		return int64(n * 1024 * 1024)
	case "GB", "GIB":
		return int64(n * 1024 * 1024 * 1024)
	default:
		return int64(n)
	}
}

type settingWriter interface {
	Set(ctx context.Context, key, value string) error
	Get(ctx context.Context, key string) (string, error)
}

func seedSetting(ctx context.Context, sw settingWriter, key, fallback string) {
	if _, err := sw.Get(ctx, key); err == nil {
		return
	}
	_ = sw.Set(ctx, key, fallback)
}

func getSetting(ctx context.Context, sr settingWriter, key, fallback string) string {
	v, err := sr.Get(ctx, key)
	if err != nil || v == "" {
		return fallback
	}
	return v
}
