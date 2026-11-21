package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/doc"
	"github.com/thesis-shared/api/internal/identity"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
)

// DocumentHandler 提供文档、成员、快照与批注接口。
type DocumentHandler struct {
	service *doc.Service
	// tokens 用于签发协同服务所需的短期 JWT（见 CollabToken）。
	tokens *identity.TokenManager
	// wsPublicURL 是浏览器连接协同服务的地址。
	wsPublicURL string
}

func NewDocumentHandler(service *doc.Service, tokens *identity.TokenManager, wsPublicURL string) *DocumentHandler {
	return &DocumentHandler{service: service, tokens: tokens, wsPublicURL: wsPublicURL}
}

// ---------------------------------------------------------------------------
// 文档 CRUD
// ---------------------------------------------------------------------------

// Create 处理 POST /api/v1/groups/{slug}/documents，请求体 {title}。
func (h *DocumentHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		WriteError(w, http.StatusNotFound, "group_not_found", "群组不存在")
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	created, err := h.service.CreateDocument(r.Context(), userID, slug, req.Title)
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"document": created})
}

// Get 处理 GET /api/v1/documents/{id}，返回文档与当前用户角色。
func (h *DocumentHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	found, role, err := h.service.GetDocument(r.Context(), userID, docID)
	if err != nil {
		writeDocError(w, err)
		return
	}
	// 契约要求 myRole 在无角色时为 null；这里 GetDocument 只在可查看时返回，
	// role 必然非空，但仍以指针形式输出保持形状稳定。
	writeJSON(w, http.StatusOK, map[string]any{"document": found, "myRole": role})
}

// Update 处理 PATCH /api/v1/documents/{id}，请求体 {title?, status?}。
func (h *DocumentHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Title  *string `json:"title"`
		Status *string `json:"status"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	updated, err := h.service.UpdateDocument(r.Context(), userID, docID, doc.UpdateDocumentInput{
		Title: req.Title, Status: req.Status,
	})
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"document": updated})
}

// Delete 处理 DELETE /api/v1/documents/{id}，软删除，仅 owner。
func (h *DocumentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteDocument(r.Context(), userID, docID); err != nil {
		writeDocError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// List 处理 GET /api/v1/groups/{slug}/documents?cursor=&limit=20。
func (h *DocumentHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		WriteError(w, http.StatusNotFound, "group_not_found", "群组不存在")
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListDocuments(r.Context(), userID, slug, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// 文档成员
// ---------------------------------------------------------------------------

// Members 处理 GET /api/v1/documents/{id}/members。
func (h *DocumentHandler) Members(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListMembers(r.Context(), userID, docID)
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// UpdateMember 处理 PATCH /api/v1/documents/{id}/members/{userId}，请求体 {role}。
func (h *DocumentHandler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, targetID, ok := h.docAndMember(w, r)
	if !ok {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	member, err := h.service.UpdateMemberRole(r.Context(), userID, docID, targetID, req.Role)
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"member": member})
}

// RemoveMember 处理 DELETE /api/v1/documents/{id}/members/{userId}。
func (h *DocumentHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, targetID, ok := h.docAndMember(w, r)
	if !ok {
		return
	}
	if err := h.service.RemoveMember(r.Context(), userID, docID, targetID); err != nil {
		writeDocError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// 快照
// ---------------------------------------------------------------------------

// Snapshots 处理 GET /api/v1/documents/{id}/snapshots?cursor=&limit=20。
func (h *DocumentHandler) Snapshots(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListSnapshots(r.Context(), userID, docID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// CreateSnapshot 处理 POST /api/v1/documents/{id}/snapshots，请求体 {label?}。
func (h *DocumentHandler) CreateSnapshot(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Label string `json:"label"`
	}
	// label 可选，允许空请求体。
	if !decodeOptionalRequest(w, r, &req) {
		return
	}
	snapshot, err := h.service.CreateSnapshot(r.Context(), userID, docID, req.Label)
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"snapshot": snapshot})
}

// ---------------------------------------------------------------------------
// 批注
// ---------------------------------------------------------------------------

// Annotations 处理 GET /api/v1/documents/{id}/annotations。
func (h *DocumentHandler) Annotations(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListAnnotations(r.Context(), userID, docID)
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// CreateAnnotation 处理 POST /api/v1/documents/{id}/annotations。
func (h *DocumentHandler) CreateAnnotation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Anchor     json.RawMessage `json:"anchor"`
		QuotedText string          `json:"quotedText"`
		Content    string          `json:"content"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	created, err := h.service.CreateAnnotation(r.Context(), userID, docID, doc.CreateAnnotationInput{
		Anchor: req.Anchor, QuotedText: req.QuotedText, Content: req.Content,
	})
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"annotation": created})
}

// UpdateAnnotation 处理 PATCH /api/v1/documents/{id}/annotations/{aid}。
func (h *DocumentHandler) UpdateAnnotation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	annotationID, err := uuid.Parse(chi.URLParam(r, "aid"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "批注 ID 格式不正确")
		return
	}
	var req struct {
		Content  *string `json:"content"`
		Resolved *bool   `json:"resolved"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	updated, err := h.service.UpdateAnnotation(r.Context(), userID, docID, annotationID, doc.UpdateAnnotationInput{
		Content: req.Content, Resolved: req.Resolved,
	})
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"annotation": updated})
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

func requireUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return uuid.Nil, false
	}
	return userID, true
}

func docIDFromPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	docID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "文档 ID 格式不正确")
		return uuid.Nil, false
	}
	return docID, true
}

func (h *DocumentHandler) docAndMember(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	targetID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "用户 ID 格式不正确")
		return uuid.Nil, uuid.Nil, false
	}
	return docID, targetID, true
}

func writeDocError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, doc.ErrValidation), errors.Is(err, doc.ErrInvalidCursor):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, doc.ErrGroupNotFound):
		status, code, message = http.StatusNotFound, "group_not_found", "群组不存在"
	case errors.Is(err, doc.ErrNotFound):
		// 文档不存在与无权查看统一 404，避免泄露存在性。
		status, code, message = http.StatusNotFound, "document_not_found", "文档不存在或无权查看"
	case errors.Is(err, doc.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "没有权限执行该操作"
	case errors.Is(err, doc.ErrMemberNotFound):
		status, code, message = http.StatusNotFound, "member_not_found", "该用户不是文档成员"
	case errors.Is(err, doc.ErrOwnerImmutable):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, doc.ErrAnnotationNotFound):
		status, code, message = http.StatusNotFound, "annotation_not_found", "批注不存在"
	default:
		slog.Error("文档操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}

// CollabToken 处理 GET /api/v1/documents/{id}/collab-token。
//
// 前端需要 JWT 才能连接协同服务（WebSocket），但会话保存在 HttpOnly Cookie 中，
// 浏览器脚本读不到。因此这里在用户已登录（Cookie 有效）的前提下，签发一个
// 短期令牌交给前端专门用于 WS 握手。
//
// 权限：必须是文档可访问者。不可访问时与 Get 一致返回 404，避免探测文档是否存在。
func (h *DocumentHandler) CollabToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	if _, _, err := h.service.GetDocument(r.Context(), userID, docID); err != nil {
		writeDocError(w, err)
		return
	}
	if h.tokens == nil {
		WriteError(w, http.StatusServiceUnavailable, "collab_unavailable", "协同服务未配置")
		return
	}
	token, err := h.tokens.Issue(userID)
	if err != nil {
		slog.Error("签发协同令牌失败", "err", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"wsUrl": h.wsPublicURL,
	})
}
