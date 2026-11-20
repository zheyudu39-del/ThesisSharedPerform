package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
	"github.com/thesis-shared/api/internal/notify"
)

// Handler 提供通知中心接口：列表、未读数、已读与偏好。
//
// 所有路由都要求登录：通知天然属于个人数据，没有匿名读的语义。
type Handler struct{ service *notify.Service }

func NewHandler(service *notify.Service) *Handler {
	return &Handler{service: service}
}

// List 处理 GET /api/v1/notifications。
// 查询参数：cursor（可选）、limit（1-50，默认 20）。
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.List(r.Context(), userID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// UnreadCount 处理 GET /api/v1/notifications/unread-count。
func (h *Handler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	count, err := h.service.UnreadCount(r.Context(), userID)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unreadCount": count})
}

// MarkRead 处理 PATCH /api/v1/notifications/{id}/read。
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	notificationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "通知 ID 格式不正确")
		return
	}
	if err := h.service.MarkRead(r.Context(), userID, notificationID); err != nil {
		writeNotifyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MarkAllRead 处理 POST /api/v1/notifications/read-all。
func (h *Handler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	if err := h.service.MarkAllRead(r.Context(), userID); err != nil {
		writeNotifyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetPrefs 处理 GET /api/v1/notifications/prefs。
func (h *Handler) GetPrefs(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	prefs, err := h.service.GetPrefs(r.Context(), userID)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prefs": prefs})
}

type updatePrefsRequest struct {
	// 用指针区分「未提供」与「清空」：缺省表示本次不改动，[] 表示取消全部静音。
	MutedTypes  *[]string `json:"mutedTypes"`
	EmailDigest string    `json:"emailDigest"`
}

// UpdatePrefs 处理 PATCH /api/v1/notifications/prefs。
func (h *Handler) UpdatePrefs(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req updatePrefsRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	var mutedTypes []string
	if req.MutedTypes != nil {
		mutedTypes = *req.MutedTypes
		// 空数组不是 nil，需要显式保留「清空」语义。
		if mutedTypes == nil {
			mutedTypes = []string{}
		}
	}
	if err := h.service.UpdatePrefs(r.Context(), userID, mutedTypes, req.EmailDigest); err != nil {
		writeNotifyError(w, err)
		return
	}
	prefs, err := h.service.GetPrefs(r.Context(), userID)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prefs": prefs})
}

func (h *Handler) currentUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return uuid.Nil, false
	}
	return userID, true
}

func writeNotifyError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, notify.ErrValidation), errors.Is(err, notify.ErrInvalidCursor):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, notify.ErrNotificationNotFound):
		status, code, message = http.StatusNotFound, "notification_not_found", "通知不存在"
	default:
		slog.Error("通知操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}
