package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/content"
	"github.com/thesis-shared/api/internal/identity"
	"github.com/thesis-shared/api/internal/openapi"
	"github.com/thesis-shared/api/internal/paper"
)

// OpenAPIHandler 提供对外开放的只读数据端点。
//
// 与内部 API 的区别：
//   - 认证走 X-API-Key，不读 Cookie；
//   - 返回结构更扁平、更稳定，对外开放后不宜频繁变动；
//   - 只暴露 public 内容，不返回任何与「当前用户」相关的字段。
type OpenAPIHandler struct {
	keys     *openapi.Service
	papers   *paper.Service
	contents *content.Service
	users    *identity.Service
}

// NewOpenAPIHandler 构造开放 API 处理器。
func NewOpenAPIHandler(
	keys *openapi.Service,
	papers *paper.Service,
	contents *content.Service,
	users *identity.Service,
) *OpenAPIHandler {
	return &OpenAPIHandler{keys: keys, papers: papers, contents: contents, users: users}
}

// ---------------------------------------------------------------------------
// 密钥管理（走普通会话认证，不属于 /openapi/v1）
// ---------------------------------------------------------------------------

// ListKeys 处理 GET /api/v1/api-keys。
func (h *OpenAPIHandler) ListKeys(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	keys, err := h.keys.ListKeys(r.Context(), userID)
	if err != nil {
		writeOpenAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// CreateKey 处理 POST /api/v1/api-keys。
// 明文只在本响应里出现一次。
func (h *OpenAPIHandler) CreateKey(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Name      string     `json:"name"`
		Scopes    []string   `json:"scopes"`
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	created, err := h.keys.CreateKey(r.Context(), userID, openapi.CreateKeyInput{
		Name: req.Name, Scopes: req.Scopes, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		writeOpenAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// RevokeKey 处理 DELETE /api/v1/api-keys/{id}。
func (h *OpenAPIHandler) RevokeKey(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	keyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "密钥 ID 格式不正确")
		return
	}
	if err := h.keys.RevokeKey(r.Context(), userID, keyID); err != nil {
		writeOpenAPIError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// 公开数据（挂在 /openapi/v1，由 APIKeyAuth 注入身份）
// ---------------------------------------------------------------------------

// openPaper 是开放的论文投影。
type openPaper struct {
	ID            string   `json:"id"`
	DOI           string   `json:"doi,omitempty"`
	ArxivID       string   `json:"arxivId,omitempty"`
	OpenreviewID  string   `json:"openreviewId,omitempty"`
	Title         string   `json:"title"`
	Authors       []string `json:"authors"`
	Abstract      string   `json:"abstract,omitempty"`
	Venue         string   `json:"venue,omitempty"`
	PublishedYear int      `json:"publishedYear,omitempty"`
	CitationCount int      `json:"citationCount"`
	URL           string   `json:"url"`
}

// ListPapers 处理 GET /openapi/v1/papers?limit=&offset=。
func (h *OpenAPIHandler) ListPapers(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := openPagination(w, r)
	if !ok {
		return
	}
	records, total, err := h.papers.List(r.Context(), limit, offset)
	if err != nil {
		writeOpenAPIError(w, err)
		return
	}
	items := make([]openPaper, 0, len(records))
	for _, record := range records {
		items = append(items, toOpenPaper(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"papers": items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetPaper 处理 GET /openapi/v1/papers/{id}。
func (h *OpenAPIHandler) GetPaper(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "论文 ID 格式不正确")
		return
	}
	record, err := h.papers.GetByID(r.Context(), id)
	if err != nil {
		writeOpenAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paper": toOpenPaper(record)})
}

// openPost 是开放的帖子投影。
//
// 刻意不含 liked / bookmarked 这类「与查看者相关」的字段：
// 开放 API 没有用户上下文，返回它们只会造成误解。
type openPost struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`
	Author       string  `json:"author"`
	AuthorHandle string  `json:"authorHandle"`
	ContentText  string  `json:"contentText"`
	LikeCount    int32   `json:"likeCount"`
	CommentCount int32   `json:"commentCount"`
	PaperID      *string `json:"paperId,omitempty"`
	CreatedAt    string  `json:"createdAt"`
	URL          string  `json:"url"`
}

// ListPosts 处理 GET /openapi/v1/posts?limit=。
//
// 复用内部信息流的 "all" 档：它只返回 public 帖子，
// 可见性谓词已经在 SQL 里，不必在开放 API 里再实现一遍。
func (h *OpenAPIHandler) ListPosts(w http.ResponseWriter, r *http.Request) {
	limit, _, ok := openPagination(w, r)
	if !ok {
		return
	}
	// 匿名 userID（uuid.Nil）让可见性谓词退化为「仅 public」。
	result, err := h.contents.Feed(r.Context(), uuid.Nil, "all", "", limit)
	if err != nil {
		writeOpenAPIError(w, err)
		return
	}
	items := make([]openPost, 0, len(result.Posts))
	for _, post := range result.Posts {
		items = append(items, toOpenPost(post))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"posts":      items,
		"nextCursor": result.NextCursor,
	})
}

// GetUser 处理 GET /openapi/v1/users/{handle}。
//
// 用户资料由 handler 层直接查 identity 服务不现实（这里没有那个依赖），
// 因此复用公开主页所需的字段集：只返回可以对外展示的列。
func (h *OpenAPIHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")
	if handle == "" {
		WriteError(w, http.StatusNotFound, "not_found", "用户不存在")
		return
	}
	// 匿名查看：GetPublic 只返回可以对外展示的列，
	// 不含 email / role / status 等账号信息。
	user, stats, err := h.users.GetPublic(r.Context(), handle)
	if err != nil {
		writeOpenAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":             user.ID.String(),
			"name":           user.Name,
			"handle":         user.Handle,
			"bio":            user.Bio,
			"institution":    user.Institution,
			"researchFields": user.ResearchFields,
			"verifyType":     user.VerifyType,
			"createdAt":      user.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			"postCount":      stats.PostCount,
			"followerCount":  stats.FollowerCount,
			"followingCount": stats.FollowingCount,
		},
	})
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

func openPagination(w http.ResponseWriter, r *http.Request) (limit, offset int32, ok bool) {
	query := r.URL.Query()
	limit = 20
	if raw := query.Get("limit"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || value <= 0 || value > 100 {
			WriteError(w, http.StatusUnprocessableEntity, "validation_error", "limit 必须是 1-100 的整数")
			return 0, 0, false
		}
		limit = int32(value)
	}
	if raw := query.Get("offset"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || value < 0 {
			WriteError(w, http.StatusUnprocessableEntity, "validation_error", "offset 必须是非负整数")
			return 0, 0, false
		}
		offset = int32(value)
	}
	return limit, offset, true
}

func toOpenPaper(p paper.Paper) openPaper {
	authors := make([]string, 0, len(p.Authors))
	for _, author := range p.Authors {
		authors = append(authors, author.Name)
	}
	url := ""
	switch {
	case p.DOI != "":
		url = "https://doi.org/" + p.DOI
	case p.ArxivID != "":
		url = "https://arxiv.org/abs/" + p.ArxivID
	}
	return openPaper{
		ID:            p.ID.String(),
		DOI:           p.DOI,
		ArxivID:       p.ArxivID,
		OpenreviewID:  p.OpenreviewID,
		Title:         p.Title,
		Authors:       authors,
		Abstract:      p.Abstract,
		Venue:         p.Venue,
		PublishedYear: p.PublishedYear,
		CitationCount: p.CitationCount,
		URL:           url,
	}
}

func toOpenPost(p content.Post) openPost {
	out := openPost{
		ID:           p.ID.String(),
		Type:         p.Type,
		Author:       p.Author.Name,
		AuthorHandle: p.Author.Handle,
		ContentText:  p.ContentText,
		LikeCount:    p.LikeCount,
		CommentCount: p.CommentCount,
		CreatedAt:    p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		URL:          "/posts/" + p.ID.String(),
	}
	if p.Paper != nil {
		id := p.Paper.ID.String()
		out.PaperID = &id
	}
	return out
}

func writeOpenAPIError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, openapi.ErrValidation):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, openapi.ErrUnauthorized):
		status, code, message = http.StatusUnauthorized, "unauthorized", "API Key 无效"
	case errors.Is(err, openapi.ErrScopeDenied):
		status, code, message = http.StatusForbidden, "insufficient_scope", "该 API Key 没有访问此资源的权限"
	case errors.Is(err, openapi.ErrKeyNotFound):
		status, code, message = http.StatusNotFound, "key_not_found", "API Key 不存在"
	case errors.Is(err, openapi.ErrNotFound), errors.Is(err, paper.ErrNotFound), errors.Is(err, content.ErrPostNotFound):
		status, code, message = http.StatusNotFound, "not_found", "资源不存在"
	default:
		slog.Error("开放 API 操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}
