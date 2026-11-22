package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/admin"
	"github.com/thesis-shared/api/internal/file"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
)

// AdminHandler 提供运营后台接口：概览统计、用户管理、内容管理、文件排查。
//
// 路由层只保证「已登录」；角色判定在 admin 服务层完成（requireAdmin），
// 与 moderation 的做法一致。这样新增接口时即使漏挂中间件，也不会变成越权入口。
type AdminHandler struct {
	service *admin.Service
	// files 用于「重新转码」。为 nil 时该接口返回 503，
	// 其余后台功能不受影响（对象存储是可选依赖）。
	files *file.Service
}

// NewAdminHandler 构造处理器。
func NewAdminHandler(service *admin.Service) *AdminHandler {
	return &AdminHandler{service: service}
}

// SetFileService 注入文件服务，供重新转码使用。
func (h *AdminHandler) SetFileService(files *file.Service) { h.files = files }

/* -------------------------------------------------------------------------- */
/* 概览                                                                       */
/* -------------------------------------------------------------------------- */

// Overview 处理 GET /api/v1/admin/overview?days=14。
func (h *AdminHandler) Overview(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	days, ok := adminQueryInt(w, r, "days", 14, 1, 90)
	if !ok {
		return
	}
	result, err := h.service.GetOverview(r.Context(), userID, int32(days))
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

/* -------------------------------------------------------------------------- */
/* 用户                                                                       */
/* -------------------------------------------------------------------------- */

// Users 处理 GET /api/v1/admin/users。
func (h *AdminHandler) Users(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	limit, ok := adminQueryInt(w, r, "limit", 20, 1, 100)
	if !ok {
		return
	}
	q := r.URL.Query()
	result, err := h.service.ListUsers(r.Context(), userID, admin.UserFilter{
		Query:  q.Get("q"),
		Role:   q.Get("role"),
		Status: q.Get("status"),
		Cursor: q.Get("cursor"),
		Limit:  int32(limit),
	})
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type setUserRoleRequest struct {
	Role string `json:"role"`
}

// UpdateUserRole 处理 PATCH /api/v1/admin/users/{id}/role。
func (h *AdminHandler) UpdateUserRole(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	targetID, ok := adminPathUUID(w, r, "id", "用户 ID")
	if !ok {
		return
	}
	var req setUserRoleRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	result, err := h.service.SetUserRole(r.Context(), userID, targetID, req.Role)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": result})
}

type setUserStatusRequest struct {
	Status string `json:"status"`
}

// UpdateUserStatus 处理 PATCH /api/v1/admin/users/{id}/status（封禁 / 解封）。
func (h *AdminHandler) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	targetID, ok := adminPathUUID(w, r, "id", "用户 ID")
	if !ok {
		return
	}
	var req setUserStatusRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	result, err := h.service.SetUserStatus(r.Context(), userID, targetID, req.Status)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": result})
}

/* -------------------------------------------------------------------------- */
/* 内容                                                                       */
/* -------------------------------------------------------------------------- */

// Posts 处理 GET /api/v1/admin/posts。
func (h *AdminHandler) Posts(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	filter, ok := adminContentFilter(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListPosts(r.Context(), userID, filter)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// DeletePost 处理 DELETE /api/v1/admin/posts/{id}（软删除）。
func (h *AdminHandler) DeletePost(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	postID, ok := adminPathUUID(w, r, "id", "帖子 ID")
	if !ok {
		return
	}
	if err := h.service.DeletePost(r.Context(), userID, postID); err != nil {
		writeAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RestorePost 处理 POST /api/v1/admin/posts/{id}/restore。
func (h *AdminHandler) RestorePost(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	postID, ok := adminPathUUID(w, r, "id", "帖子 ID")
	if !ok {
		return
	}
	if err := h.service.RestorePost(r.Context(), userID, postID); err != nil {
		writeAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Papers 处理 GET /api/v1/admin/papers。
func (h *AdminHandler) Papers(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	filter, ok := adminContentFilter(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListPapers(r.Context(), userID, filter)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Comments 处理 GET /api/v1/admin/comments。
func (h *AdminHandler) Comments(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	filter, ok := adminContentFilter(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListComments(r.Context(), userID, filter)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// DeleteComment 处理 DELETE /api/v1/admin/comments/{id}（软删除）。
func (h *AdminHandler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	commentID, ok := adminPathUUID(w, r, "id", "评论 ID")
	if !ok {
		return
	}
	if err := h.service.DeleteComment(r.Context(), userID, commentID); err != nil {
		writeAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RestoreComment 处理 POST /api/v1/admin/comments/{id}/restore。
func (h *AdminHandler) RestoreComment(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	commentID, ok := adminPathUUID(w, r, "id", "评论 ID")
	if !ok {
		return
	}
	if err := h.service.RestoreComment(r.Context(), userID, commentID); err != nil {
		writeAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Groups 处理 GET /api/v1/admin/groups。
func (h *AdminHandler) Groups(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	filter, ok := adminContentFilter(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListGroups(r.Context(), userID, filter)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// DeleteGroup 处理 DELETE /api/v1/admin/groups/{id}（软删除）。
func (h *AdminHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	groupID, ok := adminPathUUID(w, r, "id", "群组 ID")
	if !ok {
		return
	}
	if err := h.service.DeleteGroup(r.Context(), userID, groupID); err != nil {
		writeAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RestoreGroup 处理 POST /api/v1/admin/groups/{id}/restore。
func (h *AdminHandler) RestoreGroup(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	groupID, ok := adminPathUUID(w, r, "id", "群组 ID")
	if !ok {
		return
	}
	if err := h.service.RestoreGroup(r.Context(), userID, groupID); err != nil {
		writeAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/* -------------------------------------------------------------------------- */
/* 文件                                                                       */
/* -------------------------------------------------------------------------- */

// Files 处理 GET /api/v1/admin/files。
func (h *AdminHandler) Files(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	limit, ok := adminQueryInt(w, r, "limit", 20, 1, 100)
	if !ok {
		return
	}
	q := r.URL.Query()
	result, err := h.service.ListFiles(r.Context(), userID, admin.FileFilter{
		Status:      q.Get("status"),
		OnlyProblem: q.Get("onlyProblem") == "true",
		Cursor:      q.Get("cursor"),
		Limit:       int32(limit),
	})
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// RetryFile 处理 POST /api/v1/admin/files/{id}/retry，重新触发转码。
//
// 转码本身属于 file 服务，后台只负责「确认调用者是运营」再委托过去。
// 放在 admin 前缀下而不是复用 /files/{id}/complete：后者要求文件归属，
// 而运营排查时往往要重试别人上传的文件。
func (h *AdminHandler) RetryFile(w http.ResponseWriter, r *http.Request) {
	userID, ok := adminUser(w, r)
	if !ok {
		return
	}
	fileID, ok := adminPathUUID(w, r, "id", "文件 ID")
	if !ok {
		return
	}
	if h.files == nil {
		WriteError(w, http.StatusServiceUnavailable, "storage_unavailable", "文件服务暂时不可用")
		return
	}
	// 先确认运营身份，再触达 file 服务：否则任何人都能触发转码。
	if err := h.service.CheckAdmin(r.Context(), userID); err != nil {
		writeAdminError(w, err)
		return
	}
	result, err := h.files.RetryTranscode(r.Context(), fileID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": result})
}

/* -------------------------------------------------------------------------- */
/* 工具                                                                       */
/* -------------------------------------------------------------------------- */

// adminUser 取当前登录用户；未登录返回 401 并已写好响应。
func adminUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return uuid.Nil, false
	}
	return userID, true
}

// adminPathUUID 解析路径里的 UUID 参数。
func adminPathUUID(w http.ResponseWriter, r *http.Request, param, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", label+" 格式不正确")
		return uuid.Nil, false
	}
	return id, true
}

// adminQueryInt 解析整数查询参数，缺省用 def，越界报 422。
func adminQueryInt(w http.ResponseWriter, r *http.Request, name string, def, min, max int) (int, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error",
			name+" 须为 "+strconv.Itoa(min)+" 到 "+strconv.Itoa(max)+" 的整数")
		return 0, false
	}
	return n, true
}

// adminContentFilter 组装内容列表的通用筛选参数。
func adminContentFilter(w http.ResponseWriter, r *http.Request) (admin.ContentFilter, bool) {
	limit, ok := adminQueryInt(w, r, "limit", 20, 1, 100)
	if !ok {
		return admin.ContentFilter{}, false
	}
	q := r.URL.Query()
	return admin.ContentFilter{
		Query:          q.Get("q"),
		Cursor:         q.Get("cursor"),
		Limit:          int32(limit),
		IncludeDeleted: q.Get("includeDeleted") == "true",
	}, true
}

// writeAdminError 把服务层错误映射为 HTTP 响应。
func writeAdminError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, admin.ErrValidation), errors.Is(err, admin.ErrInvalidCursor):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, admin.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "没有权限执行该操作"
	case errors.Is(err, admin.ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "记录不存在"
	case errors.Is(err, admin.ErrLastAdmin):
		status, code, message = http.StatusConflict, "last_admin", err.Error()
	default:
		slog.Error("后台操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}
