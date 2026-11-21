package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
	"github.com/thesis-shared/api/internal/moderation"
)

// ModerationHandler 提供举报与屏蔽接口。
//
// 角色判定（仅 admin 可查看 / 处理举报）在服务层完成，
// 路由只需要保证「已登录」——未登录请求根本到不了这里。
type ModerationHandler struct{ service *moderation.Service }

func NewModerationHandler(service *moderation.Service) *ModerationHandler {
	return &ModerationHandler{service: service}
}

// ---------------------------------------------------------------------------
// 举报
// ---------------------------------------------------------------------------

type createReportRequest struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	Reason     string `json:"reason"`
	Detail     string `json:"detail"`
}

// CreateReport 处理 POST /api/v1/reports。
func (h *ModerationHandler) CreateReport(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	var req createReportRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	targetID, err := uuid.Parse(strings.TrimSpace(req.TargetID))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "targetId 格式不正确")
		return
	}
	created, err := h.service.CreateReport(r.Context(), userID, moderation.CreateReportInput{
		TargetType: req.TargetType, TargetID: targetID, Reason: req.Reason, Detail: req.Detail,
	})
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"report": created})
}

// ListReports 处理 GET /api/v1/reports?status=&cursor=&limit=，仅运营。
func (h *ModerationHandler) ListReports(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListReports(r.Context(), userID, r.URL.Query().Get("status"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// UpdateReport 处理 PATCH /api/v1/reports/{id}，仅运营。
func (h *ModerationHandler) UpdateReport(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	reportID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "举报 ID 格式不正确")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	updated, err := h.service.UpdateReportStatus(r.Context(), userID, reportID, req.Status)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": updated})
}

// ---------------------------------------------------------------------------
// 屏蔽
// ---------------------------------------------------------------------------

// Block 处理 POST /api/v1/users/{handle}/block。
func (h *ModerationHandler) Block(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	if err := h.service.Block(r.Context(), userID, chi.URLParam(r, "handle")); err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"blocked": true})
}

// Unblock 处理 DELETE /api/v1/users/{handle}/block。
func (h *ModerationHandler) Unblock(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	if err := h.service.Unblock(r.Context(), userID, chi.URLParam(r, "handle")); err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"blocked": false})
}

// ListBlocks 处理 GET /api/v1/blocks。
func (h *ModerationHandler) ListBlocks(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListBlocks(r.Context(), userID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// GetBlockStatus 处理 GET /api/v1/users/{handle}/block。
//
// 个人主页需要初始化「屏蔽 / 已屏蔽」按钮，但公开主页是匿名可读的，
// 因此这个状态只能单独查：匿名访问返回 401，前端按未登录处理。
func (h *ModerationHandler) GetBlockStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	blocked, err := h.service.Blocked(r.Context(), userID, chi.URLParam(r, "handle"))
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"blocked": blocked})
}

// ---------------------------------------------------------------------------
// 错误映射
// ---------------------------------------------------------------------------

func writeModerationError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, moderation.ErrValidation), errors.Is(err, moderation.ErrInvalidCursor):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, moderation.ErrReportNotFound):
		status, code, message = http.StatusNotFound, "report_not_found", "举报不存在"
	case errors.Is(err, moderation.ErrAlreadyReported):
		status, code, message = http.StatusConflict, "already_reported", err.Error()
	case errors.Is(err, moderation.ErrSelfReport):
		status, code, message = http.StatusUnprocessableEntity, "self_report", err.Error()
	case errors.Is(err, moderation.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "没有权限执行该操作"
	case errors.Is(err, moderation.ErrBlocked):
		status, code, message = http.StatusConflict, "already_blocked", err.Error()
	case errors.Is(err, moderation.ErrNotBlocked):
		status, code, message = http.StatusNotFound, "not_blocked", err.Error()
	case errors.Is(err, moderation.ErrUserNotFound):
		status, code, message = http.StatusNotFound, "user_not_found", "用户不存在"
	default:
		slog.Error("举报 / 屏蔽操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}
