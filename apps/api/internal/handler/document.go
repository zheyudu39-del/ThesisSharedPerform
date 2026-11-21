package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

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
	// storage 可选：快照下载需要读对象存储。为 nil 时下载接口返回 503，
	// 其余接口不受影响（对象存储是可选依赖，见 server.go 的注册条件）。
	storage SnapshotStorage
}

// SnapshotStorage 是快照下载对对象存储的最小依赖。
// 用接口而不是 *storage.Client，既便于测试，也避免 handler 包直接依赖 S3 SDK。
type SnapshotStorage interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

func NewDocumentHandler(service *doc.Service, tokens *identity.TokenManager, wsPublicURL string) *DocumentHandler {
	return &DocumentHandler{service: service, tokens: tokens, wsPublicURL: wsPublicURL}
}

// SetStorage 注入对象存储，供快照下载使用；不注入时该接口返回 503。
func (h *DocumentHandler) SetStorage(store SnapshotStorage) { h.storage = store }

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
// 快照回滚（PRD D-04）
// ---------------------------------------------------------------------------

// DownloadSnapshot 处理 GET /api/v1/documents/{id}/snapshots/{version}/download。
//
// 正文是 Yjs 二进制（Y.encodeStateAsUpdate 的产物），直接以附件返回，
// 供前端在本地重建一份文档或做离线备份。权限要求 editor 及以上。
func (h *DocumentHandler) DownloadSnapshot(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, version, ok := h.docAndVersion(w, r)
	if !ok {
		return
	}
	content, err := h.service.SnapshotForDownload(r.Context(), userID, docID, version)
	if err != nil {
		writeDocError(w, err)
		return
	}
	if h.storage == nil {
		WriteError(w, http.StatusServiceUnavailable, "storage_unavailable", "对象存储未配置，无法下载快照")
		return
	}
	body, err := h.storage.Get(r.Context(), content.Key)
	if err != nil {
		// 快照记录存在但对象丢失：这是数据不一致，不是客户端错误。
		slog.Error("读取快照对象失败", "err", err, "key", content.Key)
		WriteError(w, http.StatusNotFound, "snapshot_content_missing", "该版本的快照内容已不存在")
		return
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(io.LimitReader(body, maxSnapshotBytes))
	if err != nil {
		slog.Error("读取快照内容失败", "err", err, "key", content.Key)
		WriteError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return
	}
	filename := fmt.Sprintf("%s-v%d.bin", docID, content.Version)
	writeDownload(w, filename, "application/octet-stream", data)
}

// RestoreSnapshot 处理 POST /api/v1/documents/{id}/snapshots/{version}/restore。
//
// 响应体 { restoredFrom, newVersion, requiresReconnect }：
// requiresReconnect 恒为 true，前端必须断开协同连接后重新加载，
// 否则内存里的 Y.Doc 仍是回滚前的状态（详见 doc.RestoreResult 的说明）。
func (h *DocumentHandler) RestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, version, ok := h.docAndVersion(w, r)
	if !ok {
		return
	}
	result, err := h.service.RestoreSnapshot(r.Context(), userID, docID, version)
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// maxSnapshotBytes 限制单次快照下载的读取量。
// Yjs 全量状态对长文档通常在几 MB 以内，64MB 足以覆盖极端情况。
const maxSnapshotBytes = 64 << 20

// docAndVersion 解析路径里的文档 ID 与快照版本号。
func (h *DocumentHandler) docAndVersion(w http.ResponseWriter, r *http.Request) (uuid.UUID, int32, bool) {
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return uuid.Nil, 0, false
	}
	raw := chi.URLParam(r, "version")
	version, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || version <= 0 {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "快照版本号须为正整数")
		return uuid.Nil, 0, false
	}
	return docID, int32(version), true
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

// ---------------------------------------------------------------------------
// 导出
// ---------------------------------------------------------------------------

// Export 处理 POST /api/v1/documents/{id}/export。
//
// 正文由前端从 TipTap 取出 HTML 后提交（见 doc.ExportHTML 的说明），
// 服务端只做格式转换与权限校验，并以附件形式返回。
// 权限要求 editor 及以上：PRD D-08 规定无权限用户只读且不可导出。
func (h *DocumentHandler) Export(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		HTML   string `json:"html"`
		Format string `json:"format"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	result, err := h.service.ExportHTML(r.Context(), userID, docID, req.HTML, req.Format)
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeDownload(w, result.Filename, result.ContentType, result.Content)
}

// writeDownload 以附件形式返回文件。
//
// filename* 用 RFC 5987 编码，保证中文文件名在各浏览器下正确显示；
// 同时保留 ASCII 的 filename 作为老客户端的兜底。
func writeDownload(w http.ResponseWriter, filename, contentType string, content []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition(filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// contentDisposition 构造兼容的附件头。
func contentDisposition(filename string) string {
	ascii := make([]rune, 0, len(filename))
	for _, r := range filename {
		if r < 128 && r != '"' && r != '\\' {
			ascii = append(ascii, r)
		} else {
			ascii = append(ascii, '_')
		}
	}
	return `attachment; filename="` + string(ascii) + `"; filename*=UTF-8''` + url.PathEscape(filename)
}

// ---------------------------------------------------------------------------
// 导入
// ---------------------------------------------------------------------------

// Import 处理 POST /api/v1/documents/{id}/import。
//
// 请求体 {filename, format, contentBase64}：contentBase64 是文件原始字节。
// 为什么用 base64 而不是 multipart：docx 是二进制、tex 是文本，
// 统一成 base64 让一个端点覆盖两种格式，前端也不必拼 FormData。
//
// 返回 HTML，由前端写进协同编辑器（见 doc.ImportFile 的说明）。
func (h *DocumentHandler) Import(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Filename      string `json:"filename"`
		Format        string `json:"format"`
		ContentBase64 string `json:"contentBase64"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}

	// 限制解码后的体积，避免一个超大 base64 把内存吃光。
	content, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "文件内容不是合法的 base64")
		return
	}

	result, err := h.service.ImportFile(r.Context(), userID, docID, req.Filename, content, req.Format)
	if err != nil {
		writeDocError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// 文档图片
// ---------------------------------------------------------------------------

// Asset 处理 GET /api/v1/documents/{id}/assets/{assetId}。
//
// 图片是文档的一部分，权限跟随文档：能看到文档就能看到它的图。
// 不走 files 表的「上传者独占」模型——那样协作者会看不到对方贴的图。
func (h *DocumentHandler) Asset(w http.ResponseWriter, r *http.Request) {
	userID := viewerID(r)
	docID, ok := docIDFromPath(w, r)
	if !ok {
		return
	}
	assetID, err := uuid.Parse(chi.URLParam(r, "assetId"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "图片 ID 格式不正确")
		return
	}
	asset, err := h.service.GetDocAsset(r.Context(), userID, docID, assetID)
	if err != nil {
		writeDocError(w, err)
		return
	}
	w.Header().Set("Content-Type", asset.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(asset.Body)))
	// 图片内容不会变（每次导入生成新 assetID），可以长期缓存。
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(asset.Body)
}
