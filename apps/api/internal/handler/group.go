package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/group"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
)

// GroupHandler 提供群组、成员、邀请码、入群申请与群内动态接口。
type GroupHandler struct{ service *group.Service }

func NewGroupHandler(service *group.Service) *GroupHandler {
	return &GroupHandler{service: service}
}

// ---------------------------------------------------------------------------
// 群组 CRUD
// ---------------------------------------------------------------------------

type createGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	JoinPolicy  string `json:"joinPolicy"`
}

// Create 处理 POST /api/v1/groups。
func (h *GroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	var req createGroupRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	created, err := h.service.CreateGroup(r.Context(), userID, group.CreateGroupInput{
		Name: req.Name, Description: req.Description, JoinPolicy: req.JoinPolicy,
	})
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"group": created})
}

// Get 处理 GET /api/v1/groups/{slug}，公开可读。
// 契约额外返回 memberCount 与 myRole；它们已包含在 GroupDTO 内，
// 这里在顶层再给一份，兼容按契约读取顶层字段的客户端。
func (h *GroupHandler) Get(w http.ResponseWriter, r *http.Request) {
	slug, ok := groupSlug(w, r)
	if !ok {
		return
	}
	found, err := h.service.GetGroup(r.Context(), viewerID(r), slug)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group":       found,
		"myRole":      found.MyRole,
		"memberCount": found.MemberCount,
	})
}

type updateGroupRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	JoinPolicy  *string `json:"joinPolicy"`
}

// Update 处理 PATCH /api/v1/groups/{slug}。
func (h *GroupHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := groupSlug(w, r)
	if !ok {
		return
	}
	var req updateGroupRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	updated, err := h.service.UpdateGroup(r.Context(), userID, slug, group.UpdateGroupInput{
		Name: req.Name, Description: req.Description, JoinPolicy: req.JoinPolicy,
	})
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": updated})
}

// List 处理 GET /api/v1/groups，公开可读，仅列出 joinPolicy='public' 的群。
func (h *GroupHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListGroups(r.Context(), viewerID(r), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// 成员
// ---------------------------------------------------------------------------

// Members 处理 GET /api/v1/groups/{slug}/members。
func (h *GroupHandler) Members(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := groupSlug(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListMembers(r.Context(), userID, slug, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// UpdateMember 处理 PATCH /api/v1/groups/{slug}/members/{userId}。
func (h *GroupHandler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, targetID, ok := h.groupAndMember(w, r)
	if !ok {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	member, err := h.service.UpdateMemberRole(r.Context(), userID, slug, targetID, req.Role)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"member": member})
}

// RemoveMember 处理 DELETE /api/v1/groups/{slug}/members/{userId}。
func (h *GroupHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, targetID, ok := h.groupAndMember(w, r)
	if !ok {
		return
	}
	if err := h.service.RemoveMember(r.Context(), userID, slug, targetID); err != nil {
		writeGroupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// 邀请
// ---------------------------------------------------------------------------

// CreateInvite 处理 POST /api/v1/groups/{slug}/invites。
func (h *GroupHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := groupSlug(w, r)
	if !ok {
		return
	}
	var req struct {
		ExpiresInHours *int32 `json:"expiresInHours"`
		MaxUses        *int32 `json:"maxUses"`
	}
	// 两个字段都可选，空请求体表示全部使用默认值（168 小时、不限次数）。
	if !decodeOptionalRequest(w, r, &req) {
		return
	}
	invite, err := h.service.CreateInvite(r.Context(), userID, slug, group.CreateInviteInput{
		ExpiresInHours: req.ExpiresInHours, MaxUses: req.MaxUses,
	})
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"invite": invite})
}

// JoinByInvite 处理 POST /api/v1/groups/join，请求体 {code}。
func (h *GroupHandler) JoinByInvite(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	joined, err := h.service.JoinByInvite(r.Context(), userID, req.Code)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": joined})
}

// JoinPublic 处理 POST /api/v1/groups/{slug}/join，直接加入公开群。
func (h *GroupHandler) JoinPublic(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := groupSlug(w, r)
	if !ok {
		return
	}
	joined, err := h.service.JoinPublic(r.Context(), userID, slug)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": joined})
}

// ---------------------------------------------------------------------------
// 入群申请
// ---------------------------------------------------------------------------

// CreateRequest 处理 POST /api/v1/groups/{slug}/requests。
func (h *GroupHandler) CreateRequest(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := groupSlug(w, r)
	if !ok {
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	// 留言可选，允许空请求体。
	if !decodeOptionalRequest(w, r, &req) {
		return
	}
	created, err := h.service.CreateJoinRequest(r.Context(), userID, slug, req.Message)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"request": created})
}

// Requests 处理 GET /api/v1/groups/{slug}/requests。
func (h *GroupHandler) Requests(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := groupSlug(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListJoinRequests(r.Context(), userID, slug)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ApproveRequest 处理 POST /api/v1/groups/{slug}/requests/{id}/approve。
func (h *GroupHandler) ApproveRequest(w http.ResponseWriter, r *http.Request) {
	h.reviewRequest(w, r, true)
}

// RejectRequest 处理 POST /api/v1/groups/{slug}/requests/{id}/reject。
func (h *GroupHandler) RejectRequest(w http.ResponseWriter, r *http.Request) {
	h.reviewRequest(w, r, false)
}

func (h *GroupHandler) reviewRequest(w http.ResponseWriter, r *http.Request, approve bool) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := groupSlug(w, r)
	if !ok {
		return
	}
	requestID, err := uuid.Parse(chi.URLParam(r, "requestId"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "申请 ID 格式不正确")
		return
	}
	result, err := h.service.ReviewJoinRequest(r.Context(), userID, slug, requestID, approve)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": result})
}

// ---------------------------------------------------------------------------
// 群内动态
// ---------------------------------------------------------------------------

// Feed 处理 GET /api/v1/groups/{slug}/feed，仅成员可读。
func (h *GroupHandler) Feed(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	slug, ok := groupSlug(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.Feed(r.Context(), userID, slug, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// decodeOptionalRequest 与 decodeRequest 相同，但允许请求体为空。
//
// 创建邀请码与提交入群申请的全部字段都是可选的，客户端常直接 POST 空体；
// 此时应视为「全部使用默认值」，而不是 422。
func decodeOptionalRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	defer func() { _ = r.Body.Close() }()
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "请求体读取失败")
		return false
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "请求体格式不正确")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "请求体只能包含一个 JSON 对象")
		return false
	}
	return true
}

func groupSlug(w http.ResponseWriter, r *http.Request) (string, bool) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		WriteError(w, http.StatusNotFound, "group_not_found", "群组不存在")
		return "", false
	}
	return slug, true
}

func (h *GroupHandler) groupAndMember(w http.ResponseWriter, r *http.Request) (string, uuid.UUID, bool) {
	slug, ok := groupSlug(w, r)
	if !ok {
		return "", uuid.Nil, false
	}
	targetID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "用户 ID 格式不正确")
		return "", uuid.Nil, false
	}
	return slug, targetID, true
}

func writeGroupError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, group.ErrValidation), errors.Is(err, group.ErrInvalidCursor):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, group.ErrGroupNotFound):
		status, code, message = http.StatusNotFound, "group_not_found", "群组不存在"
	case errors.Is(err, group.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "没有权限执行该操作"
	case errors.Is(err, group.ErrOwnerImmutable):
		// 契约要求「不能修改 owner 角色」返回 422。
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, group.ErrMemberNotFound):
		status, code, message = http.StatusNotFound, "member_not_found", "该用户不是群组成员"
	case errors.Is(err, group.ErrAlreadyMember):
		status, code, message = http.StatusConflict, "already_member", "已经是群组成员"
	case errors.Is(err, group.ErrInviteNotFound):
		status, code, message = http.StatusNotFound, "invite_not_found", "邀请码不存在或已失效"
	case errors.Is(err, group.ErrAlreadyRequested):
		status, code, message = http.StatusConflict, "already_requested", "已提交过入群申请，请等待审核"
	case errors.Is(err, group.ErrJoinPolicy):
		status, code, message = http.StatusForbidden, "join_policy_mismatch", err.Error()
	case errors.Is(err, group.ErrRequestNotFound):
		status, code, message = http.StatusNotFound, "request_not_found", "入群申请不存在或已处理"
	default:
		slog.Error("群组操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}
