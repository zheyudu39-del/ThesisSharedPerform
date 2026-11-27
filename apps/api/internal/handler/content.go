package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/thesis-shared/api/internal/content"
	appmiddleware "github.com/thesis-shared/api/internal/middleware"
)

// ContentHandler 提供信息流、发帖、评论与互动接口。
type ContentHandler struct{ service *content.Service }

func NewContentHandler(service *content.Service) *ContentHandler {
	return &ContentHandler{service: service}
}

type createPostRequest struct {
	Content    string   `json:"content"`
	PaperID    *string  `json:"paperId"`
	RepostOf   *string  `json:"repostOf"`
	GroupID    *string  `json:"groupId"`
	Visibility string   `json:"visibility"`
	Tags       []string `json:"tags"`
	// FileIDs 是随帖发布的附件（须已上传完成且属于作者）。
	FileIDs []string `json:"fileIds"`
}

func (h *ContentHandler) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	var req createPostRequest
	// 正文限 5000 字符，但富文本 HTML 带标记后可能膨胀数倍；
	// 附件列表也在这个请求体里。64KB 在极端情况下会误伤，留 1MB 余量。
	if !decodeRequestLimit(w, r, &req, 1<<20) {
		return
	}
	paperID, ok := parseOptionalUUID(w, req.PaperID, "paperId")
	if !ok {
		return
	}
	repostOf, ok := parseOptionalUUID(w, req.RepostOf, "repostOf")
	if !ok {
		return
	}
	groupID, ok := parseOptionalUUID(w, req.GroupID, "groupId")
	if !ok {
		return
	}
	// 附件 ID 逐个解析：格式错误直接 422，而不是静默丢弃——
	// 用户以为挂上了图，实际没有，比报错更让人困惑。
	fileIDs := make([]uuid.UUID, 0, len(req.FileIDs))
	for _, raw := range req.FileIDs {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "validation_error", "附件 ID 格式不正确")
			return
		}
		fileIDs = append(fileIDs, parsed)
	}
	post, err := h.service.CreatePost(r.Context(), userID, content.CreatePostInput{Content: req.Content, PaperID: paperID, RepostOf: repostOf, GroupID: groupID, Visibility: req.Visibility, Tags: req.Tags, FileIDs: fileIDs})
	if err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"post": post})
}

func (h *ContentHandler) Feed(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	stream := r.URL.Query().Get("stream")
	if stream == "" {
		stream = "following"
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.Feed(r.Context(), userID, stream, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// UserPosts 处理 GET /api/v1/users/{handle}/posts?type=posts|reposts。
//
// 个人主页的「发帖 / 转发」Tab（PRD A-03）。公开可读：别人的主页不该
// 要求登录才能看；带会话时会顺便填上 liked / bookmarked 标记。
func (h *ContentHandler) UserPosts(w http.ResponseWriter, r *http.Request) {
	viewerID, _ := appmiddleware.UserID(r.Context())

	kind := r.URL.Query().Get("type")
	if kind == "" {
		kind = "posts"
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.AuthorPosts(
		r.Context(), viewerID, chi.URLParam(r, "handle"), kind,
		r.URL.Query().Get("cursor"), limit,
	)
	if err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// PaperPosts 处理 GET /api/v1/papers/{id}/posts?cursor=&limit=。
//
// 论文详情页的「相关讨论」（PRD P-03）。公开可读：论文是公开内容，
// 讨论列表也应匿名可见；带会话时填充 liked / bookmarked。
func (h *ContentHandler) PaperPosts(w http.ResponseWriter, r *http.Request) {
	viewerID, _ := appmiddleware.UserID(r.Context())

	paperID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "论文 ID 格式不正确")
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.PaperPosts(
		r.Context(), viewerID, paperID, r.URL.Query().Get("cursor"), limit,
	)
	if err != nil {
		writeContentError(w, err)
		return
	}

	// 首页（无游标）时顺带返回总数，前端页头直接显示「共 N 条讨论」。
	// 翻页不重复统计：省一次全表扫描，且数字在会话内保持一致。
	if r.URL.Query().Get("cursor") == "" {
		if total, err := h.service.CountPaperPosts(r.Context(), viewerID, paperID); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"posts": result.Posts, "nextCursor": result.NextCursor, "total": total,
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// Tag 处理 GET /api/v1/tags/{slug}，返回标签详情与公开帖子数（PRD F-07）。
func (h *ContentHandler) Tag(w http.ResponseWriter, r *http.Request) {
	tag, err := h.service.GetTag(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tag": tag})
}

// TagPosts 处理 GET /api/v1/tags/{slug}/posts，话题页的帖子列表。
func (h *ContentHandler) TagPosts(w http.ResponseWriter, r *http.Request) {
	viewerID, _ := appmiddleware.UserID(r.Context())
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.TagPosts(
		r.Context(), viewerID, chi.URLParam(r, "slug"),
		r.URL.Query().Get("cursor"), limit,
	)
	if err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// TopTags 处理 GET /api/v1/tags，按公开帖子数排序的热门标签。
func (h *ContentHandler) TopTags(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	tags, err := h.service.TopTags(r.Context(), limit)
	if err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func (h *ContentHandler) Like(w http.ResponseWriter, r *http.Request)   { h.setLike(w, r, true) }
func (h *ContentHandler) Unlike(w http.ResponseWriter, r *http.Request) { h.setLike(w, r, false) }
func (h *ContentHandler) setLike(w http.ResponseWriter, r *http.Request, liked bool) {
	userID, postID, ok := h.userAndPost(w, r)
	if !ok {
		return
	}
	count, err := h.service.SetLike(r.Context(), userID, postID, liked)
	if err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"liked": liked, "likeCount": count})
}

func (h *ContentHandler) Bookmark(w http.ResponseWriter, r *http.Request) { h.setBookmark(w, r, true) }
func (h *ContentHandler) Unbookmark(w http.ResponseWriter, r *http.Request) {
	h.setBookmark(w, r, false)
}
func (h *ContentHandler) setBookmark(w http.ResponseWriter, r *http.Request, bookmarked bool) {
	userID, postID, ok := h.userAndPost(w, r)
	if !ok {
		return
	}
	if err := h.service.SetBookmark(r.Context(), userID, postID, bookmarked); err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"bookmarked": bookmarked})
}

// Bookmarks 处理 GET /api/v1/bookmarks?cursor=&limit=。
//
// 收藏是个人数据，必须登录；游标编码收藏时间，按收藏时间倒序返回，
// 响应形状与 /feed 一致（posts + nextCursor），前端可直接复用 PostCard。
func (h *ContentHandler) Bookmarks(w http.ResponseWriter, r *http.Request) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.Bookmarks(r.Context(), userID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *ContentHandler) CreateComment(w http.ResponseWriter, r *http.Request) {
	userID, postID, ok := h.userAndPost(w, r)
	if !ok {
		return
	}
	var req struct {
		Content  string  `json:"content"`
		ParentID *string `json:"parentId"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	parentID, ok := parseOptionalUUID(w, req.ParentID, "parentId")
	if !ok {
		return
	}
	comment, err := h.service.CreateComment(r.Context(), userID, postID, req.Content, parentID)
	if err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"comment": comment})
}

// ListComments 处理 GET /api/v1/posts/{id}/comments。
// 公开读接口：匿名访问按 uuid.Nil 查看者处理，服务层只放行 public 帖子的评论。
func (h *ContentHandler) ListComments(w http.ResponseWriter, r *http.Request) {
	postID, ok := postIDFromPath(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	result, err := h.service.ListComments(r.Context(), viewerID(r), postID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// GetPost 处理 GET /api/v1/posts/{id}。
// 帖子不存在与无权查看返回完全相同的 404，避免通过响应差异探测私密帖。
// 匿名访问按 uuid.Nil 查看者处理，服务层只返回 public 帖。
func (h *ContentHandler) GetPost(w http.ResponseWriter, r *http.Request) {
	postID, ok := postIDFromPath(w, r)
	if !ok {
		return
	}
	post, err := h.service.GetPost(r.Context(), viewerID(r), postID)
	if err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"post": post})
}

// viewerID 返回当前查看者，匿名访问返回 uuid.Nil。
//
// 公开读路由挂在 OptionalAuthenticate 下，缺少令牌是正常路径而非错误：
// 服务层把 uuid.Nil 编码成 SQL NULL，可见性谓词于是只剩 visibility = 'public'。
func viewerID(r *http.Request) uuid.UUID {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		return uuid.Nil
	}
	return userID
}

// postIDFromPath 解析路径中的帖子 ID。
func postIDFromPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "帖子 ID 格式不正确")
		return uuid.Nil, false
	}
	return postID, true
}

// userAndPost 用于仍需登录的写接口：缺少令牌直接 401。
func (h *ContentHandler) userAndPost(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := appmiddleware.UserID(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return uuid.Nil, uuid.Nil, false
	}
	postID, ok := postIDFromPath(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return userID, postID, true
}

func parseOptionalUUID(w http.ResponseWriter, raw *string, field string) (*uuid.UUID, bool) {
	if raw == nil || *raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(*raw)
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", field+" 格式不正确")
		return nil, false
	}
	return &id, true
}

func parseLimit(w http.ResponseWriter, r *http.Request) (int32, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 20, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > 50 {
		WriteError(w, http.StatusUnprocessableEntity, "validation_error", "limit 须为 1 到 50")
		return 0, false
	}
	return int32(limit), true
}

func writeContentError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "服务暂时不可用"
	switch {
	case errors.Is(err, content.ErrValidation), errors.Is(err, content.ErrInvalidCursor):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, content.ErrPaperNotFound):
		status, code, message = http.StatusNotFound, "paper_not_found", "论文不存在"
	case errors.Is(err, content.ErrRepostNotFound):
		status, code, message = http.StatusNotFound, "repost_not_found", "原帖不存在或无权访问"
	case errors.Is(err, content.ErrGroupNotMember):
		status, code, message = http.StatusForbidden, "group_forbidden", "不是该群组成员"
	case errors.Is(err, content.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "只有作者或运营可以操作这条帖子"
	case errors.Is(err, content.ErrPostNotFound), errors.Is(err, content.ErrParentNotFound):
		status, code, message = http.StatusNotFound, "post_not_found", err.Error()
	default:
		slog.Error("内容操作失败", "err", err)
	}
	WriteError(w, status, code, message)
}

// ---------------------------------------------------------------------------
// 帖子编辑与删除
// ---------------------------------------------------------------------------

// updatePostRequest 是 PATCH /api/v1/posts/{id} 的请求体。
//
// 三个正文字段都接受，按 content → contentHtml → contentText 的优先级取第一个
// 非 nil 的作为编辑后的正文。无论走哪个字段，服务层都会重新跑
// SanitizeContent（Markdown → bluemonday），因此即便客户端回传 contentHtml
// 也无法注入未清洗的标签。
// 用指针区分「没传」与「传了空字符串」：后者是合法的「清空正文」请求。
type updatePostRequest struct {
	Content     *string `json:"content"`
	ContentHTML *string `json:"contentHtml"`
	ContentText *string `json:"contentText"`
	Visibility  *string `json:"visibility"`
}

// UpdatePost 处理 PATCH /api/v1/posts/{id}。
//
// 仅作者本人或运营可改；可改字段为正文与可见性。
// 权限语义：不存在 404，非作者非运营 403（帖子是公开的，隐藏存在性没有意义）。
func (h *ContentHandler) UpdatePost(w http.ResponseWriter, r *http.Request) {
	userID, postID, ok := h.userAndPost(w, r)
	if !ok {
		return
	}
	var req updatePostRequest
	// 与 CreatePost 同理：富文本 HTML 可能远超纯文本长度。
	if !decodeRequestLimit(w, r, &req, 1<<20) {
		return
	}
	input := content.UpdatePostInput{Visibility: req.Visibility}
	switch {
	case req.Content != nil:
		input.Content = req.Content
	case req.ContentHTML != nil:
		input.Content = req.ContentHTML
	case req.ContentText != nil:
		input.Content = req.ContentText
	}
	post, err := h.service.UpdatePost(r.Context(), userID, postID, input)
	if err != nil {
		writeContentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"post": post})
}

// DeletePost 处理 DELETE /api/v1/posts/{id}。
//
// 软删除，成功返回 204（无响应体）。仅作者本人或运营可删。
func (h *ContentHandler) DeletePost(w http.ResponseWriter, r *http.Request) {
	userID, postID, ok := h.userAndPost(w, r)
	if !ok {
		return
	}
	if err := h.service.DeletePost(r.Context(), userID, postID); err != nil {
		writeContentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
