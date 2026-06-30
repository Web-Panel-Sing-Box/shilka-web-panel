package handler

import (
	"bufio"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/services/singbox"
)

type CoreHandler struct {
	pm          singbox.ProcessManager
	applier     *singbox.Applier
	log         *slog.Logger
	coreLogPath string
	notifier    singbox.FailureNotifier
}

func NewCoreHandler(pm singbox.ProcessManager, applier *singbox.Applier, log *slog.Logger, coreLogPath string, notifiers ...singbox.FailureNotifier) *CoreHandler {
	h := &CoreHandler{pm: pm, applier: applier, log: log, coreLogPath: coreLogPath}
	if len(notifiers) > 0 {
		h.notifier = notifiers[0]
	}
	return h
}

func (h *CoreHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/core/status", h.Status)
	mux.HandleFunc("POST /api/core/start", h.Start)
	mux.HandleFunc("POST /api/core/stop", h.Stop)
	mux.HandleFunc("POST /api/core/restart", h.Restart)
	mux.HandleFunc("POST /api/core/reload", h.Reload)
	mux.HandleFunc("GET /api/core/version", h.Version)
	mux.HandleFunc("GET /api/core/config", h.Config)
	mux.HandleFunc("GET /api/core/logs", h.Logs)
}

type coreStatusDTO struct {
	Running       bool   `json:"running"`
	PID           int    `json:"pid"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
	LastError     string `json:"lastError,omitempty"`
}

// Status godoc
//
//	@Summary	Core process status
//	@Tags		core
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	coreStatusDTO
//	@Router		/core/status [get]
func (h *CoreHandler) Status(w http.ResponseWriter, r *http.Request) {
	st, err := h.pm.Status(r.Context())
	if err != nil {
		h.log.Error("core status", slog.String("error", h.safeError(r, err.Error())))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, coreStatusDTO{
		Running:       st.Running,
		PID:           st.PID,
		Version:       st.Version,
		UptimeSeconds: int64(st.Uptime.Seconds()),
		LastError:     h.safeError(r, st.LastError),
	})
}

func (h *CoreHandler) action(w http.ResponseWriter, r *http.Request, op string, fn func() error) {
	if err := fn(); err != nil {
		safe := h.safeError(r, err.Error())
		h.log.Error("core "+op, slog.String("error", safe))
		if h.notifier != nil {
			h.notifier.NotifyFailure(r.Context(), coreEvent(op), err)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": safe})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": op + " ok"})
}

// Start godoc
//
//	@Summary	Start the core
//	@Description	Generates the initial config (if missing), then starts the core process.
//	@Tags		core
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]string
//	@Router		/core/start [post]
func (h *CoreHandler) Start(w http.ResponseWriter, r *http.Request) {
	if err := h.applier.ApplyIfMissing(r.Context()); err != nil {
		safe := h.safeError(r, err.Error())
		h.log.Error("core start: apply config", slog.String("error", safe))
		if h.notifier != nil {
			h.notifier.NotifyFailure(r.Context(), domain.NotificationEventStart, err)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "apply config: " + safe})
		return
	}
	h.action(w, r, "start", func() error { return h.pm.Start(r.Context()) })
}

// Stop godoc
//
//	@Summary	Stop the core
//	@Tags		core
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]string
//	@Router		/core/stop [post]
func (h *CoreHandler) Stop(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "stop", func() error { return h.pm.Stop(r.Context()) })
}

// Restart godoc
//
//	@Summary	Restart the core
//	@Tags		core
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]string
//	@Router		/core/restart [post]
func (h *CoreHandler) Restart(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "restart", func() error { return h.pm.Restart(r.Context()) })
}

// Reload godoc
//
//	@Summary	Regenerate, validate and apply the config
//	@Description	Renders config from the database, runs `sing-box check`, installs it and reloads the core. A failed check returns 400 with the validation error and leaves the live config untouched.
//	@Tags		core
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]string
//	@Failure	400	{object}	map[string]string
//	@Router		/core/reload [post]
func (h *CoreHandler) Reload(w http.ResponseWriter, r *http.Request) {
	if err := h.applier.Apply(r.Context()); err != nil {
		if h.notifier != nil {
			h.notifier.NotifyFailure(r.Context(), domain.NotificationEventReload, err)
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": h.safeError(r, err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "config applied"})
}

func coreEvent(op string) string {
	switch op {
	case "start":
		return domain.NotificationEventStart
	case "stop":
		return domain.NotificationEventStop
	default:
		return domain.NotificationEventRestart
	}
}

func (h *CoreHandler) safeError(r *http.Request, text string) string {
	if redactor, ok := h.notifier.(singbox.ErrorRedactor); ok {
		return redactor.RedactError(r.Context(), text)
	}
	return text
}

// Version godoc
//
//	@Summary	Core version
//	@Tags		core
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]string
//	@Router		/core/version [get]
func (h *CoreHandler) Version(w http.ResponseWriter, r *http.Request) {
	st, _ := h.pm.Status(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{"version": st.Version})
}

// Config godoc
//
//	@Summary	Preview the generated config
//	@Tags		core
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]any
//	@Router		/core/config [get]
func (h *CoreHandler) Config(w http.ResponseWriter, r *http.Request) {
	data, err := h.applier.Preview(r.Context())
	if err != nil {
		h.log.Error("core config preview", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

type coreLogsResponse struct {
	Lines   []string `json:"lines"`
	Total   int      `json:"total"`
	HasMore bool     `json:"hasMore"`
}

// Logs godoc
//
//	@Summary	Core process logs
//	@Description	Returns lines from the sing-box log file with pagination.
//	@Tags		core
//	@Produce	json
//	@Security	BearerAuth
//	@Param		offset	query		int	false	"Line offset"	default(0)
//	@Param		limit	query		int	false	"Lines per page"	default(200)
//	@Success	200		{object}	coreLogsResponse
//	@Router		/core/logs [get]
func (h *CoreHandler) Logs(w http.ResponseWriter, r *http.Request) {
	if h.coreLogPath == "" {
		writeJSON(w, http.StatusOK, coreLogsResponse{Lines: []string{}, Total: 0, HasMore: false})
		return
	}

	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 200
	}

	lines, total, err := readLogLines(h.coreLogPath, offset, limit)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, coreLogsResponse{Lines: []string{}, Total: 0, HasMore: false})
			return
		}
		h.log.Error("core logs", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if offset >= total {
		writeJSON(w, http.StatusOK, coreLogsResponse{Lines: []string{}, Total: total, HasMore: false})
		return
	}

	writeJSON(w, http.StatusOK, coreLogsResponse{
		Lines:   lines,
		Total:   total,
		HasMore: offset+len(lines) < total,
	})
}

func readLogLines(path string, offset, limit int) ([]string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lines := make([]string, 0, limit)
	total := 0
	for scanner.Scan() {
		if total >= offset && len(lines) < limit {
			lines = append(lines, scanner.Text())
		}
		total++
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	return lines, total, nil
}
