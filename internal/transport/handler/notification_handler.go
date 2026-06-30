package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"sing-box-web-panel/internal/services/notification"
)

type NotificationHandler struct {
	svc *notification.Service
	log *slog.Logger
}

func NewNotificationHandler(svc *notification.Service, log *slog.Logger) *NotificationHandler {
	return &NotificationHandler{svc: svc, log: log}
}

func (h *NotificationHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/notifications", h.Get)
	mux.HandleFunc("PUT /api/settings/notifications", h.Put)
	mux.HandleFunc("POST /api/settings/notifications/test", h.Test)
}

// Get godoc
//
//	@Summary	Get notification settings without secrets
//	@Tags		settings
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	notification.View
//	@Router		/settings/notifications [get]
func (h *NotificationHandler) Get(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.Get(r.Context())
	if err != nil {
		h.log.Error("notification settings get", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load notification settings"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Put godoc
//
//	@Summary	Update notification settings
//	@Description	Empty secrets preserve configured values. clearPassword and clearBotToken remove them explicitly.
//	@Tags		settings
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		notification.Update	true	"Notification settings"
//	@Success	200		{object}	notification.View
//	@Failure	400		{object}	map[string]string
//	@Router		/settings/notifications [put]
func (h *NotificationHandler) Put(w http.ResponseWriter, r *http.Request) {
	var request notification.Update
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := h.svc.Update(r.Context(), request)
	if err != nil {
		if errors.Is(err, notification.ErrValidation) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		h.log.Error("notification settings put", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save notification settings"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type notificationTestRequest struct {
	Channel string `json:"channel"`
}

// Test godoc
//
//	@Summary	Send a test notification
//	@Tags		settings
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		notificationTestRequest	true	"Channel"
//	@Success	200		{object}	notification.LastTestView
//	@Failure	400		{object}	map[string]string
//	@Failure	502		{object}	notification.LastTestView
//	@Router		/settings/notifications/test [post]
func (h *NotificationHandler) Test(w http.ResponseWriter, r *http.Request) {
	var request notificationTestRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := h.svc.Test(r.Context(), request.Channel)
	if err != nil {
		if errors.Is(err, notification.ErrValidation) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadGateway, result)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
