package content

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/db"
	"github.com/thesis-shared/api/internal/paper"
)

var (
	ErrValidation     = errors.New("输入校验失败")
	ErrPaperNotFound  = errors.New("论文不存在")
	ErrRepostNotFound = errors.New("原帖不存在")
	ErrGroupNotMember = errors.New("不是该群组成员")
	ErrPostNotFound   = errors.New("帖子不存在")
	ErrParentNotFound = errors.New("父评论不存在")
	// ErrForbidden 表示「帖子存在，但当前用户无权修改/删除」。
	// 与「不存在」区分开：帖子是公开的，隐藏存在性没有意义，
	// 明确返回 403 才能让前端提示「只有作者或运营可以操作」。
	ErrForbidden = errors.New("无权操作该帖子")
)

// Author 是帖子与评论共用的作者摘要。
type Author struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Handle     string    `json:"handle"`
	VerifyType string    `json:"verifyType"`
}

// CommentAuthor 保持评论契约不含认证字段。
type CommentAuthor struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Handle string    `json:"handle"`
}

// RepostSummary 只展开一层原帖，避免转发链无限嵌套。
type RepostSummary struct {
	ID          uuid.UUID    `json:"id"`
	AuthorName  string       `json:"authorName"`
	ContentText string       `json:"contentText"`
	Paper       *paper.Paper `json:"paper"`
}

// Post 是信息流与发帖接口的统一 DTO。
type Post struct {
	ID           uuid.UUID      `json:"id"`
	Type         string         `json:"type"`
	Author       Author         `json:"author"`
	ContentHTML  string         `json:"contentHtml"`
	ContentText  string         `json:"contentText"`
	Paper        *paper.Paper   `json:"paper"`
	RepostOf     *RepostSummary `json:"repostOf"`
	GroupID      *uuid.UUID     `json:"groupId"`
	Visibility   string         `json:"visibility"`
	LikeCount    int32          `json:"likeCount"`
	CommentCount int32          `json:"commentCount"`
	RepostCount  int32          `json:"repostCount"`
	HotScore     float32        `json:"hotScore"`
	CreatedAt    time.Time      `json:"createdAt"`
	Liked        bool           `json:"liked"`
	Bookmarked   bool           `json:"bookmarked"`
	Tags         []string       `json:"tags"`
	// Attachments 是帖子附带的文件（PDF / 图片等）。
	// 前端据此渲染「存入群文件库」等入口——此前该表建了但从未写入，
	// 导致文件库的「从帖子存入」缺少前提。
	Attachments []Attachment `json:"attachments"`
}

// Attachment 是帖子附件的对外投影。
type Attachment struct {
	FileID    uuid.UUID `json:"fileId"`
	Name      string    `json:"name"`
	Mime      string    `json:"mime"`
	Kind      string    `json:"kind"`
	SizeBytes int64     `json:"sizeBytes"`
	PageCount int32     `json:"pageCount"`
}

// Comment 是评论 DTO。
type Comment struct {
	ID          uuid.UUID     `json:"id"`
	PostID      uuid.UUID     `json:"postId"`
	ParentID    *uuid.UUID    `json:"parentId"`
	Author      CommentAuthor `json:"author"`
	ContentHTML string        `json:"contentHtml"`
	ContentText string        `json:"contentText"`
	LikeCount   int32         `json:"likeCount"`
	CreatedAt   time.Time     `json:"createdAt"`
}

type CreatePostInput struct {
	Content    string
	PaperID    *uuid.UUID
	RepostOf   *uuid.UUID
	GroupID    *uuid.UUID
	Visibility string
	Tags       []string
	// FileIDs 是要挂到帖子上的文件（须已上传完成且属于作者本人）。
	FileIDs []uuid.UUID
}

type FeedResult struct {
	Posts      []Post  `json:"posts"`
	NextCursor *string `json:"nextCursor"`
}

// bookmarkRow 是收藏列表的一行：帖子投影 + 收藏时间。
//
// 收藏列表按「收藏时间」而不是「帖子发布时间」倒序，排序键无法从帖子行本身
// 得到，因此把 reactions.created_at 一并投影出来。嵌入 GetPostByIDRow 是为了
// 复用 content 已有的 toPost 投影，保证收藏列表与信息流的 PostDTO 形状完全一致。
type bookmarkRow struct {
	db.GetPostByIDRow
	BookmarkedAt pgtype.Timestamptz `db:"bookmarked_at"`
}

type listBookmarkedPostsParams struct {
	UserID     pgtype.UUID
	CursorTime pgtype.Timestamptz
	CursorID   pgtype.UUID
	Limit      int32
}

// bookmarkQuerier 抽象「列出我收藏的帖子」。
//
// 不直接调用 db.ListBookmarkedPosts：sqlc 生成代码由接线代理统一产出，
// 本包先用自包含的原始 SQL 实现保证可编译、可运行；生成后把 poolBookmarkQuerier
// 换成 db.New(pool) 的薄封装即可，Service 逻辑无需改动。
type bookmarkQuerier interface {
	ListBookmarkedPosts(context.Context, listBookmarkedPostsParams) ([]bookmarkRow, error)
}

// bookmarkCursorPayload 是收藏列表的游标。
//
// 没有复用 cursor.go 的 EncodeCursor：那里的 stream 是固定枚举
// （following/all/hot/comments），且排序键是帖子时间；收藏列表的排序键是
// 收藏时间，混用会解出错误的分页位置。这里独立编码，stream 固定为 bookmarks。
type bookmarkCursorPayload struct {
	Version      int       `json:"v"`
	Stream       string    `json:"s"`
	BookmarkedAt time.Time `json:"t"`
	PostID       uuid.UUID `json:"id"`
}

const bookmarkCursorStream = "bookmarks"

func encodeBookmarkCursor(bookmarkedAt time.Time, postID uuid.UUID) (string, error) {
	if postID == uuid.Nil || bookmarkedAt.IsZero() {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(bookmarkCursorPayload{
		Version: 1, Stream: bookmarkCursorStream, BookmarkedAt: bookmarkedAt.UTC(), PostID: postID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeBookmarkCursor(raw string) (time.Time, uuid.UUID, error) {
	if raw == "" {
		return time.Time{}, uuid.Nil, nil
	}
	if len(raw) > 1024 {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	var c bookmarkCursorPayload
	if err := json.Unmarshal(payload, &c); err != nil || c.Version != 1 || c.Stream != bookmarkCursorStream ||
		c.PostID == uuid.Nil || c.BookmarkedAt.IsZero() {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	return c.BookmarkedAt.UTC(), c.PostID, nil
}

// listBookmarkedPostsSQL 与 db/queries/reactions.sql 中的 ListBookmarkedPosts 保持一致。
//
// 可见性谓词与信息流相同：收藏之后帖子可能被改为 followers / group / private，
// 收藏列表不能因此把它重新暴露给已无权查看的人。
const listBookmarkedPostsSQL = `
SELECT p.*, u.name AS author_name, u.handle AS author_handle, u.verify_type AS author_verify_type,
       EXISTS(SELECT 1 FROM reactions r WHERE r.user_id = $1 AND r.target_type = 'post' AND r.target_id = p.id AND r.kind = 'like') AS liked,
       EXISTS(SELECT 1 FROM reactions r WHERE r.user_id = $1 AND r.target_type = 'post' AND r.target_id = p.id AND r.kind = 'bookmark') AS bookmarked,
       COALESCE((SELECT array_agg(t.name ORDER BY t.name) FROM post_tags pt JOIN tags t ON t.id = pt.tag_id WHERE pt.post_id = p.id), '{}') AS tags,
       op.id AS paper_id_value, op.doi AS paper_doi, op.arxiv_id AS paper_arxiv_id, op.openreview_id AS paper_openreview_id,
       op.title AS paper_title, op.authors AS paper_authors, op.abstract AS paper_abstract, op.venue AS paper_venue,
       op.published_at AS paper_published_at, op.citation_count AS paper_citation_count, op.meta_source AS paper_meta_source, op.created_at AS paper_created_at,
       rp.id AS repost_id, ru.name AS repost_author_name, rp.content_text AS repost_content_text,
       rpp.id AS repost_paper_id, rpp.doi AS repost_paper_doi, rpp.arxiv_id AS repost_paper_arxiv_id, rpp.openreview_id AS repost_paper_openreview_id,
       rpp.title AS repost_paper_title, rpp.authors AS repost_paper_authors, rpp.abstract AS repost_paper_abstract, rpp.venue AS repost_paper_venue,
       rpp.published_at AS repost_paper_published_at, rpp.citation_count AS repost_paper_citation_count, rpp.meta_source AS repost_paper_meta_source, rpp.created_at AS repost_paper_created_at,
       b.created_at AS bookmarked_at
FROM reactions b
JOIN posts p ON p.id = b.target_id AND p.deleted_at IS NULL
JOIN users u ON u.id = p.author_id
LEFT JOIN papers op ON op.id = p.paper_id
LEFT JOIN posts rp ON rp.id = p.repost_of AND rp.deleted_at IS NULL
    AND (
      rp.author_id = $1
      OR rp.visibility = 'public'
      OR (rp.visibility = 'followers'
          AND EXISTS(SELECT 1 FROM follows rf WHERE rf.follower_id = $1 AND rf.followee_id = rp.author_id))
      OR (rp.visibility = 'group' AND rp.group_id IS NOT NULL
          AND EXISTS(SELECT 1 FROM group_members rgm WHERE rgm.group_id = rp.group_id AND rgm.user_id = $1))
    )
LEFT JOIN users ru ON ru.id = rp.author_id
LEFT JOIN papers rpp ON rpp.id = rp.paper_id
WHERE b.user_id = $1 AND b.target_type = 'post' AND b.kind = 'bookmark'
  AND (
    p.author_id = $1
    OR p.visibility = 'public'
    OR (p.visibility = 'followers'
        AND EXISTS(SELECT 1 FROM follows f WHERE f.follower_id = $1 AND f.followee_id = p.author_id))
    OR (p.visibility = 'group' AND p.group_id IS NOT NULL
        AND EXISTS(SELECT 1 FROM group_members gm WHERE gm.group_id = p.group_id AND gm.user_id = $1))
  )
  AND ($2::timestamptz IS NULL OR (b.created_at, b.target_id) < ($2::timestamptz, $3::uuid))
ORDER BY b.created_at DESC, b.target_id DESC
LIMIT $4
`

type poolBookmarkQuerier struct{ pool *pgxpool.Pool }

func (q poolBookmarkQuerier) ListBookmarkedPosts(ctx context.Context, arg listBookmarkedPostsParams) ([]bookmarkRow, error) {
	rows, err := q.pool.Query(ctx, listBookmarkedPostsSQL, arg.UserID, arg.CursorTime, arg.CursorID, arg.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]bookmarkRow, 0, arg.Limit)
	for rows.Next() {
		item, err := pgx.RowToStructByNameLax[bookmarkRow](rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

type CommentsResult struct {
	Comments   []Comment `json:"comments"`
	NextCursor *string   `json:"nextCursor"`
}

// SearchDocument 是要写入检索索引的最小字段集合。
//
// 与 search.IndexInput 字段一一对应，但这里重新声明一份而不是直接引用：
// content 不 import search，避免两个业务包互相依赖（search 已依赖 db，
// content 也依赖 db，直接互引会让依赖图出现环）。
type SearchDocument struct {
	EntityType string
	EntityID   uuid.UUID
	Title      string
	Body       string
}

// Indexer 是检索索引的抽象，由 server 在组装时注入 search.Service 的适配器。
// 为 nil 时（单元测试、未接入检索的部署）所有索引调用都被跳过，业务写入不受影响。
type Indexer interface {
	Index(ctx context.Context, doc SearchDocument) error
}

// NotifyInput 描述一条待生成的通知，字段与 notify.CreateInput 一一对应。
// 与 SearchDocument 同理，这里重新声明一份而不直接引用 notify 包：
// content 只认识自己的抽象，由 server 侧适配器桥接。
type NotifyInput struct {
	UserID     uuid.UUID
	Type       string
	ActorID    uuid.UUID
	TargetType string
	TargetID   uuid.UUID
	Payload    map[string]any
}

// Notifier 是通知生成的抽象，由 server 注入 notify.Service 的适配器。
// 为 nil 时跳过通知（单元测试或未接入通知的部署），业务写入不受影响。
type Notifier interface {
	Notify(ctx context.Context, input NotifyInput) error
}

// HandleResolver 把 @handle 批量解析成用户 ID。
//
// 抽成接口而不是直接调用生成查询，是为了让单测能注入固定映射，
// 同时避免 content 包与 db 生成物在查询形状上强耦合。
type HandleResolver interface {
	ResolveHandles(ctx context.Context, handles []string) (map[string]uuid.UUID, error)
}

// dbHandleResolver 用 ListUsersByHandles 生成查询解析 handle。
// 该查询只返回 active 且未删除的用户：给注销/封禁账号发通知没有意义。
type dbHandleResolver struct{ queries *db.Queries }

func (r dbHandleResolver) ResolveHandles(ctx context.Context, handles []string) (map[string]uuid.UUID, error) {
	if len(handles) == 0 {
		return nil, nil
	}
	rows, err := r.queries.ListUsersByHandles(ctx, handles)
	if err != nil {
		return nil, err
	}
	result := make(map[string]uuid.UUID, len(rows))
	for _, row := range rows {
		if row.ID.Valid {
			result[row.Handle] = uuid.UUID(row.ID.Bytes)
		}
	}
	return result, nil
}

// readQuerier 是公开读路径（帖子详情、评论列表）用到的最小查询集合。
// 单独抽出是为了让「匿名查看者 = uuid.Nil」的映射能在没有数据库的单元测试里验证。
type readQuerier interface {
	GetPostByID(context.Context, db.GetPostByIDParams) (db.GetPostByIDRow, error)
	GetPostForWrite(context.Context, db.GetPostForWriteParams) (pgtype.UUID, error)
	ListComments(context.Context, db.ListCommentsParams) ([]db.ListCommentsRow, error)
}

// Service 实现内容领域逻辑；pgx 连接池天然支持多请求并发。
type Service struct {
	pool    *pgxpool.Pool
	queries readQuerier
	// attachments 查询帖子附件。与 readQuerier 分开：
	// 现有测试替身不必为一个与主流程无关的能力补方法。
	attachments attachmentQuerier
	// bookmarks 列出「当前用户收藏了哪些帖子」。
	// 单独抽成接口而不是塞进 readQuerier：收藏列表是分页写路径之外的独立查询，
	// 且 sqlc 生成类型尚未就绪（见 poolBookmarkQuerier 的说明）。
	bookmarks bookmarkQuerier
	// indexer 可选：nil 时跳过索引，业务写入不受影响。
	indexer Indexer
	// notifier 可选：nil 时跳过通知生成。
	notifier Notifier
	// handles 可选：把 @handle 解析成用户 ID。为 nil 时跳过提及通知。
	handles HandleResolver
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{
		pool:      pool,
		queries:   db.New(pool),
		attachments: db.New(pool),
		bookmarks: poolBookmarkQuerier{pool: pool},
		handles:   dbHandleResolver{queries: db.New(pool)},
	}
}

// SetIndexer 注入检索索引实现。server 在创建内容服务后调用一次；
// 不注入时索引被静默跳过（与 MVP「索引缺失可由重建任务补齐」的约定一致）。
func (s *Service) SetIndexer(indexer Indexer) { s.indexer = indexer }

// SetNotifier 注入通知实现。server 在创建内容服务后调用一次；
// 不注入时点赞/评论不产生通知（单元测试或未接入通知的部署）。
func (s *Service) SetNotifier(notifier Notifier) { s.notifier = notifier }

// SetHandleResolver 注入 @handle 解析实现；不注入时跳过提及通知。
func (s *Service) SetHandleResolver(resolver HandleResolver) { s.handles = resolver }

// newServiceWithQuerier 用于测试注入替身；写路径仍需 pool，故只覆盖读路径。
// newServiceWithQuerier 用于测试注入替身。
// attachments 留空：测试替身场景下不查附件，attachFiles 会在 nil 时跳过。
func newServiceWithQuerier(queries readQuerier) *Service { return &Service{queries: queries} }

func uuidParam(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }
func optionalUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return uuidParam(*id)
}
func uuidPointer(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := uuid.UUID(value.Bytes)
	return &id
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// CreatePost 在一个事务中创建帖子、关联标签并增加原帖转发计数。
func (s *Service) CreatePost(ctx context.Context, authorID uuid.UUID, input CreatePostInput) (Post, error) {
	input.Content = strings.TrimSpace(input.Content)
	if utf8.RuneCountInString(input.Content) > 5000 || (input.Content == "" && input.RepostOf == nil) {
		return Post{}, fmt.Errorf("%w: 正文与转发目标不能同时为空，且正文不能超过 5000 字符", ErrValidation)
	}
	visibility := input.Visibility
	if visibility == "" {
		visibility = "public"
	}
	switch db.PostVisibility(visibility) {
	case db.PostVisibilityPublic, db.PostVisibilityFollowers, db.PostVisibilityGroup, db.PostVisibilityPrivate:
	default:
		return Post{}, fmt.Errorf("%w: 可见性不正确", ErrValidation)
	}
	if len(input.Tags) > 20 {
		return Post{}, fmt.Errorf("%w: 标签最多 20 个", ErrValidation)
	}

	type normalizedTag struct{ name, slug string }
	tags := make([]normalizedTag, 0, len(input.Tags))
	seen := make(map[string]struct{}, len(input.Tags))
	for _, raw := range input.Tags {
		name, slug, ok := NormalizeTag(raw)
		if !ok {
			return Post{}, fmt.Errorf("%w: 标签须为 1 到 64 个字符且包含文字", ErrValidation)
		}
		if _, exists := seen[slug]; exists {
			continue
		}
		seen[slug] = struct{}{}
		tags = append(tags, normalizedTag{name, slug})
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Post{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if input.PaperID != nil {
		if _, err := q.GetPaperForPost(ctx, uuidParam(*input.PaperID)); errors.Is(err, pgx.ErrNoRows) {
			return Post{}, ErrPaperNotFound
		} else if err != nil {
			return Post{}, err
		}
	}
	// 发到群组时必须确认是有效成员，否则会出现越权发帖或数据库外键 500。
	if input.GroupID != nil {
		if _, err := q.GetGroupMembership(ctx, db.GetGroupMembershipParams{
			GroupID: uuidParam(*input.GroupID), UserID: uuidParam(authorID),
		}); errors.Is(err, pgx.ErrNoRows) {
			return Post{}, ErrGroupNotMember
		} else if err != nil {
			return Post{}, err
		}
		if visibility != string(db.PostVisibilityGroup) {
			return Post{}, fmt.Errorf("%w: 发到群组的帖子可见性必须为 group", ErrValidation)
		}
	}

	postType := db.PostTypeOriginal
	if input.RepostOf != nil {
		postType = db.PostTypeRepost
		if _, err := q.GetPostForWrite(ctx, db.GetPostForWriteParams{ID: uuidParam(*input.RepostOf), AuthorID: uuidParam(authorID)}); errors.Is(err, pgx.ErrNoRows) {
			return Post{}, ErrRepostNotFound
		} else if err != nil {
			return Post{}, err
		}
	}
	htmlText, plainText := SanitizeContent(input.Content)
	var htmlParam, textParam *string
	if input.Content != "" {
		htmlParam, textParam = &htmlText, &plainText
	}
	created, err := q.CreatePost(ctx, db.CreatePostParams{
		AuthorID: uuidParam(authorID), Type: postType, ContentHtml: htmlParam, ContentText: textParam,
		PaperID: optionalUUID(input.PaperID), RepostOf: optionalUUID(input.RepostOf), GroupID: optionalUUID(input.GroupID),
		Visibility: db.PostVisibility(visibility),
	})
	if err != nil {
		return Post{}, err
	}
	// 附件：只挂作者本人且已就绪的文件。LinkPostAttachment 的 SQL 里
	// 已经用 EXISTS 做了这层校验，不属于作者或未就绪的文件会被静默跳过，
	// 而不是让整个发帖失败——用户多选了一张还没传完的图不该丢掉整条帖子。
	for i, fileID := range input.FileIDs {
		if err := q.LinkPostAttachment(ctx, db.LinkPostAttachmentParams{
			PostID: created.ID, FileID: uuidParam(fileID), Position: int16(i), OwnerID: uuidParam(authorID),
		}); err != nil {
			return Post{}, err
		}
	}

	for _, tag := range tags {
		record, err := q.UpsertTag(ctx, db.UpsertTagParams{Name: tag.name, Slug: tag.slug})
		if err != nil {
			return Post{}, err
		}
		if err := q.LinkPostTag(ctx, db.LinkPostTagParams{PostID: created.ID, TagID: record.ID}); err != nil {
			return Post{}, err
		}
	}
	// 转发通知的收件人必须在事务内取：提交后再查一次既多一次往返，
	// 也拿不到与本次写入一致的原帖状态。
	var repostAuthorID uuid.UUID
	if input.RepostOf != nil {
		if err := q.IncrementRepostCount(ctx, uuidParam(*input.RepostOf)); err != nil {
			return Post{}, err
		}
		author, err := q.GetPostAuthorForNotify(ctx, uuidParam(*input.RepostOf))
		if err == nil && author.Valid {
			repostAuthorID = uuid.UUID(author.Bytes)
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			// 查作者失败只影响通知，不能让已经写好的转发回滚。
			slog.Error("查询原帖作者失败", "postId", *input.RepostOf, "err", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Post{}, err
	}
	// 索引放在事务提交之后：索引失败不能回滚已经成功的发帖，
	// 缺失的文档可由重建任务补齐（见 search.Service 的同步策略说明）。
	s.indexPost(ctx, created.ID.Bytes, visibility, plainText)
	// 通知同样放在提交之后：通知失败不应回滚帖子。
	if input.RepostOf != nil {
		s.notifyRepost(ctx, repostAuthorID, authorID, created.ID.Bytes, *input.RepostOf)
	}
	s.notifyMentions(ctx, authorID, "post", created.ID.Bytes, plainText, nil)
	return s.GetPost(ctx, authorID, created.ID.Bytes)
}

// indexPost 把帖子正文写入检索索引。
//
// 只索引 public 帖：检索接口是匿名可读的，若把 followers / group / private
// 的正文也写进索引，未登录用户就能通过搜索读到本应受限的内容。
// 标题取正文前 80 字（rune），正文用完整纯文本；两者都为空时（例如纯转发
// 且没有附言）不写入，避免在索引里留下检索不到的空文档。
// 失败只记日志：发帖已经成功，索引问题不应表现为 500。
func (s *Service) indexPost(ctx context.Context, postID uuid.UUID, visibility, plainText string) {
	if s.indexer == nil || visibility != string(db.PostVisibilityPublic) {
		return
	}
	body := strings.TrimSpace(plainText)
	if body == "" {
		return
	}
	if err := s.indexer.Index(ctx, SearchDocument{
		EntityType: "post",
		EntityID:   postID,
		Title:      truncateRunes(body, 80),
		Body:       body,
	}); err != nil {
		slog.Error("索引帖子失败", "postId", postID, "err", err)
	}
}

// truncateRunes 按 rune 截断，避免把多字节字符切坏。
func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

// GetPost 返回单条帖子详情。
// 帖子不存在与当前用户无权查看返回同一个 ErrPostNotFound，
// 使调用方无法通过响应差异探测私密帖是否存在。
//
// userID 为 uuid.Nil 表示匿名访问：uuidParam 会把零值编码成 SQL NULL，
// 可见性谓词里的 `p.author_id = NULL`、`follows.follower_id = NULL` 等比较均为
// UNKNOWN，只有 `p.visibility = 'public'` 能成立，因此匿名只可能读到 public 帖。
// 这与「匿名访问不返回 401」的公开读契约一致，且不依赖调用方额外过滤。
func (s *Service) GetPost(ctx context.Context, userID, postID uuid.UUID) (Post, error) {
	row, err := s.queries.GetPostByID(ctx, db.GetPostByIDParams{ID: uuidParam(postID), UserID: uuidParam(userID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrPostNotFound
	}
	if err != nil {
		return Post{}, err
	}
	post, err := toPost(row)
	if err != nil {
		return Post{}, err
	}
	// 附件加载失败不应让帖子详情整个失败：正文才是主体。
	batch := []Post{post}
	if err := s.attachFiles(ctx, s.attachments, batch); err != nil {
		slog.Error("加载帖子附件失败", "postId", postID, "err", err)
		post.Attachments = []Attachment{}
		return post, nil
	}
	return batch[0], nil
}

// Feed 返回一页信息流，多取一条用于判断是否有下一页。
func (s *Service) Feed(ctx context.Context, userID uuid.UUID, stream, rawCursor string, limit int32) (FeedResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	createdAt, id, score, err := DecodeCursor(rawCursor, stream)
	if err != nil {
		return FeedResult{}, err
	}
	q := db.New(s.pool)
	timeCursor := pgtype.Timestamptz{Time: createdAt, Valid: rawCursor != ""}
	rows := make([]db.GetPostByIDRow, 0, limit+1)
	switch stream {
	case "following":
		result, err := q.ListFollowingFeed(ctx, db.ListFollowingFeedParams{UserID: uuidParam(userID), Column2: timeCursor, Column3: uuidParam(id), Limit: limit + 1})
		if err != nil {
			return FeedResult{}, err
		}
		for _, row := range result {
			rows = append(rows, db.GetPostByIDRow(row))
		}
	case "all":
		result, err := q.ListAllFeed(ctx, db.ListAllFeedParams{UserID: uuidParam(userID), Column2: timeCursor, Column3: uuidParam(id), Limit: limit + 1})
		if err != nil {
			return FeedResult{}, err
		}
		for _, row := range result {
			rows = append(rows, db.GetPostByIDRow(row))
		}
	case "hot":
		// 只在首页（无游标）重算热度分。
		//
		// 翻页途中重算会让 hot_score 变化，导致同一批帖子的排序在页与页之间漂移，
		// 出现重复或漏帖。因此首页刷新一次，后续页沿用同一份快照排序。
		// 代价是单次会话内的热度最多滞后一次翻页的时间，可接受；
		// 后续接入后台 Worker 后应改为定时刷新（见 data-model.md §4.11）。
		if rawCursor == "" {
			if err := q.RefreshHotScores(ctx); err != nil {
				return FeedResult{}, err
			}
		}
		scoreCursor := float32(math.Inf(1))
		if score != nil {
			scoreCursor = float32(*score)
		}
		result, err := q.ListHotFeed(ctx, db.ListHotFeedParams{UserID: uuidParam(userID), Column2: scoreCursor, Column3: timeCursor, Column4: uuidParam(id), Limit: limit + 1})
		if err != nil {
			return FeedResult{}, err
		}
		for _, row := range result {
			rows = append(rows, db.GetPostByIDRow(row))
		}
	default:
		return FeedResult{}, fmt.Errorf("%w: stream 只能是 following、all 或 hot", ErrValidation)
	}

	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	posts := make([]Post, 0, len(rows))
	for _, row := range rows {
		post, err := toPost(row)
		if err != nil {
			return FeedResult{}, err
		}
		posts = append(posts, post)
	}
	if err := s.attachFiles(ctx, s.attachments, posts); err != nil {
		slog.Error("加载信息流附件失败", "stream", stream, "err", err)
	}
	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		var hotScore *float64
		if stream == "hot" {
			value := float64(last.HotScore)
			hotScore = &value
		}
		encoded, err := EncodeCursor(stream, last.CreatedAt.Time, last.ID.Bytes, hotScore)
		if err != nil {
			return FeedResult{}, err
		}
		next = &encoded
	}
	return FeedResult{Posts: posts, NextCursor: next}, nil
}

// AuthorPosts 返回某作者的个人主页帖子列表（PRD A-03）。
//
// kind 取 "posts"（原创）或 "reposts"（转发）。两条查询的 SELECT 投影与
// 信息流一致，因此可以复用 toPost 与同一套游标编码。
//
// viewerID 用于 liked / bookmarked 标记；匿名访问时传 uuid.Nil，
// 此时两个标记恒为 false——这也是它们本来就该有的值。
func (s *Service) AuthorPosts(ctx context.Context, viewerID uuid.UUID, handle, kind, rawCursor string, limit int32) (FeedResult, error) {
	if kind != "posts" && kind != "reposts" {
		return FeedResult{}, fmt.Errorf("%w: kind 只能是 posts 或 reposts", ErrValidation)
	}
	if s.handles == nil {
		return FeedResult{}, fmt.Errorf("%w: 账号服务未配置", ErrValidation)
	}
	handle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	resolved, err := s.handles.ResolveHandles(ctx, []string{handle})
	if err != nil {
		return FeedResult{}, err
	}
	authorID, ok := resolved[handle]
	if !ok || authorID == uuid.Nil {
		// 不存在与已注销共用 404，避免探测账号状态。
		return FeedResult{}, ErrPostNotFound
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	createdAt, id, _, err := DecodeCursor(rawCursor, kind)
	if err != nil {
		return FeedResult{}, err
	}

	q := db.New(s.pool)
	params := db.ListAuthorPostsParams{
		ViewerID:   uuidParam(viewerID),
		AuthorID:   uuidParam(authorID),
		CursorTime: pgtype.Timestamptz{Time: createdAt, Valid: rawCursor != ""},
		CursorID:   uuidParam(id),
		PageLimit:  limit + 1,
	}

	var rows []db.GetPostByIDRow
	if kind == "posts" {
		result, err := q.ListAuthorPosts(ctx, params)
		if err != nil {
			return FeedResult{}, err
		}
		for _, row := range result {
			rows = append(rows, db.GetPostByIDRow(row))
		}
	} else {
		result, err := q.ListAuthorReposts(ctx, db.ListAuthorRepostsParams{
			ViewerID:   params.ViewerID,
			AuthorID:   params.AuthorID,
			CursorTime: params.CursorTime,
			CursorID:   params.CursorID,
			PageLimit:  params.PageLimit,
		})
		if err != nil {
			return FeedResult{}, err
		}
		for _, row := range result {
			rows = append(rows, db.GetPostByIDRow(row))
		}
	}

	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	posts := make([]Post, 0, len(rows))
	for _, row := range rows {
		post, err := toPost(row)
		if err != nil {
			return FeedResult{}, err
		}
		posts = append(posts, post)
	}
	if err := s.attachFiles(ctx, s.attachments, posts); err != nil {
		slog.Error("加载个人主页附件失败", "author", authorID, "kind", kind, "err", err)
	}

	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := EncodeCursor(kind, last.CreatedAt.Time, last.ID.Bytes, nil)
		if err != nil {
			return FeedResult{}, err
		}
		next = &encoded
	}
	return FeedResult{Posts: posts, NextCursor: next}, nil
}

// PaperPosts 返回某篇论文下的公开讨论（PRD P-03）。
//
// 只收公开且不属于群组的帖子：群内讨论是群组上下文的一部分，
// 挂到公开论文页上等于把私域内容暴露给所有人。
//
// viewerID 为 uuid.Nil 表示匿名：liked / bookmarked 恒为 false，
// 同时被屏蔽作者的帖子不做过滤（匿名者没有屏蔽列表）。
func (s *Service) PaperPosts(ctx context.Context, viewerID, paperID uuid.UUID, rawCursor string, limit int32) (FeedResult, error) {
	if paperID == uuid.Nil {
		return FeedResult{}, fmt.Errorf("%w: 论文 ID 不能为空", ErrValidation)
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	createdAt, id, _, err := DecodeCursor(rawCursor, "paper")
	if err != nil {
		return FeedResult{}, err
	}

	q := db.New(s.pool)
	rows, err := q.ListPaperPosts(ctx, db.ListPaperPostsParams{
		ViewerID:   uuidParam(viewerID),
		PaperID:    uuidParam(paperID),
		CursorTime: pgtype.Timestamptz{Time: createdAt, Valid: rawCursor != ""},
		CursorID:   uuidParam(id),
		PageLimit:  limit + 1,
	})
	if err != nil {
		return FeedResult{}, err
	}

	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	posts := make([]Post, 0, len(rows))
	for _, row := range rows {
		post, err := toPost(db.GetPostByIDRow(row))
		if err != nil {
			return FeedResult{}, err
		}
		posts = append(posts, post)
	}
	if err := s.attachFiles(ctx, s.attachments, posts); err != nil {
		slog.Error("加载论文讨论附件失败", "paper", paperID, "err", err)
	}

	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := EncodeCursor("paper", last.CreatedAt.Time, last.ID.Bytes, nil)
		if err != nil {
			return FeedResult{}, err
		}
		next = &encoded
	}
	return FeedResult{Posts: posts, NextCursor: next}, nil
}

// CountPaperPosts 返回论文讨论总数，供页面上显示「共 N 条讨论」。
func (s *Service) CountPaperPosts(ctx context.Context, viewerID, paperID uuid.UUID) (int64, error) {
	q := db.New(s.pool)
	return q.CountPaperPosts(ctx, db.CountPaperPostsParams{
		ViewerID: uuidParam(viewerID),
		PaperID:  uuidParam(paperID),
	})
}

// SetLike 原子维护 reaction 和冗余计数，重复操作幂等。
func (s *Service) SetLike(ctx context.Context, userID, postID uuid.UUID, liked bool) (int32, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if _, err := q.GetPostForWrite(ctx, db.GetPostForWriteParams{ID: uuidParam(postID), AuthorID: uuidParam(userID)}); errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrPostNotFound
	} else if err != nil {
		return 0, err
	}
	params := db.AddReactionParams{UserID: uuidParam(userID), TargetID: uuidParam(postID), Kind: "like"}
	var changed int64
	var count int32
	if liked {
		changed, err = q.AddReaction(ctx, params)
		if err == nil && changed == 1 {
			count, err = q.AddLikeCount(ctx, uuidParam(postID))
		} else if err == nil {
			count, err = q.GetLikeCount(ctx, uuidParam(postID))
		}
	} else {
		changed, err = q.RemoveReaction(ctx, db.RemoveReactionParams(params))
		if err == nil && changed == 1 {
			count, err = q.RemoveLikeCount(ctx, uuidParam(postID))
		} else if err == nil {
			count, err = q.GetLikeCount(ctx, uuidParam(postID))
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrPostNotFound
	}
	if err != nil {
		return 0, err
	}
	// 只有「本次真正新增了一个赞」才通知：重复点赞（changed == 0）不重复打扰。
	// 作者在事务内先取，提交后再发通知——通知失败不能回滚已经成功的点赞。
	var authorID uuid.UUID
	if liked && changed == 1 {
		row, lookupErr := q.GetPostAuthorForNotify(ctx, uuidParam(postID))
		switch {
		case lookupErr == nil && row.Valid:
			authorID = uuid.UUID(row.Bytes)
		case lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows):
			// 查询作者失败只影响通知，不应让点赞失败。
			slog.Error("查询帖子作者失败", "postId", postID, "err", lookupErr)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	if authorID != uuid.Nil {
		s.notifyLike(ctx, authorID, userID, postID)
	}
	return count, nil
}

// notifyLike 生成点赞聚合通知。自己给自己点赞由 notify 服务层再兜底跳过，
// 这里提前返回可以省掉一次查询。
func (s *Service) notifyLike(ctx context.Context, authorID, actorID, postID uuid.UUID) {
	if s.notifier == nil || authorID == actorID {
		return
	}
	if err := s.notifier.Notify(ctx, NotifyInput{
		UserID:     authorID,
		Type:       "like_agg",
		ActorID:    actorID,
		TargetType: "post",
		TargetID:   postID,
	}); err != nil {
		slog.Error("生成点赞通知失败", "postId", postID, "err", err)
	}
}

// SetBookmark 通过主键冲突/删除行数实现幂等，不影响帖子热度。
func (s *Service) SetBookmark(ctx context.Context, userID, postID uuid.UUID, bookmarked bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if _, err := q.GetPostForWrite(ctx, db.GetPostForWriteParams{ID: uuidParam(postID), AuthorID: uuidParam(userID)}); errors.Is(err, pgx.ErrNoRows) {
		return ErrPostNotFound
	} else if err != nil {
		return err
	}
	if bookmarked {
		_, err = q.AddReaction(ctx, db.AddReactionParams{UserID: uuidParam(userID), TargetID: uuidParam(postID), Kind: "bookmark"})
	} else {
		_, err = q.RemoveReaction(ctx, db.RemoveReactionParams{UserID: uuidParam(userID), TargetID: uuidParam(postID), Kind: "bookmark"})
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Bookmarks 返回当前用户收藏的帖子，按收藏时间倒序。
//
// 与信息流一样多取一条判断是否有下一页；游标编码的是 (收藏时间, 帖子 ID)，
// 因为排序键是收藏时间而非帖子发布时间——用户刚收藏一条旧帖，它应当排在最前。
func (s *Service) Bookmarks(ctx context.Context, userID uuid.UUID, rawCursor string, limit int32) (FeedResult, error) {
	if userID == uuid.Nil {
		return FeedResult{}, fmt.Errorf("%w: 需要登录", ErrValidation)
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	bookmarkedAt, postID, err := decodeBookmarkCursor(rawCursor)
	if err != nil {
		return FeedResult{}, err
	}
	if s.bookmarks == nil {
		// 未注入查询实现的部署（例如只读单元测试）没有收藏列表可返回，
		// 返回空页而不是 panic，保持与 feed 一致的空列表语义。
		return FeedResult{Posts: []Post{}}, nil
	}
	rows, err := s.bookmarks.ListBookmarkedPosts(ctx, listBookmarkedPostsParams{
		UserID:     uuidParam(userID),
		CursorTime: pgtype.Timestamptz{Time: bookmarkedAt, Valid: rawCursor != ""},
		CursorID:   uuidParam(postID),
		Limit:      limit + 1,
	})
	if err != nil {
		return FeedResult{}, err
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	posts := make([]Post, 0, len(rows))
	for _, row := range rows {
		post, err := toPost(row.GetPostByIDRow)
		if err != nil {
			return FeedResult{}, err
		}
		posts = append(posts, post)
	}
	if err := s.attachFiles(ctx, s.attachments, posts); err != nil {
		slog.Error("加载收藏附件失败", "err", err)
	}
	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := encodeBookmarkCursor(last.BookmarkedAt.Time, last.ID.Bytes)
		if err != nil {
			return FeedResult{}, err
		}
		next = &encoded
	}
	return FeedResult{Posts: posts, NextCursor: next}, nil
}

func (s *Service) CreateComment(ctx context.Context, userID, postID uuid.UUID, content string, parentID *uuid.UUID) (Comment, error) {
	content = strings.TrimSpace(content)
	if content == "" || utf8.RuneCountInString(content) > 5000 {
		return Comment{}, fmt.Errorf("%w: 评论须为 1 到 5000 字符", ErrValidation)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Comment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if _, err := q.GetPostForWrite(ctx, db.GetPostForWriteParams{ID: uuidParam(postID), AuthorID: uuidParam(userID)}); errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, ErrPostNotFound
	} else if err != nil {
		return Comment{}, err
	}
	if parentID != nil {
		parent, err := q.GetCommentParentInfo(ctx, db.GetCommentParentInfoParams{
			ID: uuidParam(*parentID), PostID: uuidParam(postID),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return Comment{}, ErrParentNotFound
		} else if err != nil {
			return Comment{}, err
		}
		// PRD F-04 只支持一级回复：不能回复一条已经是回复的评论。
		// 更深层的讨论折叠到根评论下，避免无限嵌套。
		if parent.ParentID.Valid {
			return Comment{}, fmt.Errorf("%w: 只能回复主评论，不支持多级嵌套", ErrValidation)
		}
	}
	htmlText, plainText := SanitizeContent(content)
	created, err := q.CreateComment(ctx, db.CreateCommentParams{PostID: uuidParam(postID), ParentID: optionalUUID(parentID), AuthorID: uuidParam(userID), ContentHtml: htmlText, ContentText: plainText})
	if err != nil {
		return Comment{}, err
	}
	if err := q.IncrementCommentCount(ctx, uuidParam(postID)); err != nil {
		return Comment{}, err
	}
	row, err := q.GetCommentByID(ctx, created.ID)
	if err != nil {
		return Comment{}, err
	}
	// 通知对象在提交前确定。两种事件：
	//   - 主评论 → 通知帖子作者（comment）；
	//   - 回复   → 通知被回复评论的作者（reply），若父评论作者就是帖子作者，
	//     只发 reply，避免同一次操作给同一个人发两条。
	// 通知在提交后发送，失败只记日志，不影响评论本身。
	var postAuthorID uuid.UUID
	if postAuthor, err := q.GetPostAuthorForNotify(ctx, uuidParam(postID)); err == nil && postAuthor.Valid {
		postAuthorID = uuid.UUID(postAuthor.Bytes)
	}
	var replyToID uuid.UUID
	if parentID != nil {
		if parentAuthor, err := q.GetCommentAuthorForNotify(ctx, uuidParam(*parentID)); err == nil && parentAuthor.Valid {
			replyToID = uuid.UUID(parentAuthor.Bytes)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Comment{}, err
	}
	if parentID != nil {
		// 回复：通知被回复评论的作者（reply）；帖子作者若另有人，再补一条 comment。
		// 父评论作者 == 帖子作者时只发 reply，避免同一次操作给同一个人发两条。
		if replyToID != uuid.Nil && replyToID != userID {
			s.notifyComment(ctx, replyToID, userID, postID, "reply")
		}
		if postAuthorID != uuid.Nil && postAuthorID != userID && postAuthorID != replyToID {
			s.notifyComment(ctx, postAuthorID, userID, postID, "comment")
		}
	} else if postAuthorID != uuid.Nil && postAuthorID != userID {
		s.notifyComment(ctx, postAuthorID, userID, postID, "comment")
	}
	// 评论正文里的 @提及同样要通知；与回复通知可能同时命中同一个人，
	// 那是两条语义不同的通知（「回复了你」与「提到了你」），不去重。
	s.notifyMentions(ctx, userID, "post", postID, plainText, nil)
	return toComment(row), nil
}

// notifyComment 生成评论/回复通知。
//
// 回复通知的 targetType 用 "comment"：前端据此知道要跳到 payload.postId
// 对应的帖子详情，而不是把通知里的评论 ID 当作帖子 ID 拼路由。
// targetID 仍保留帖子 ID，便于后端排查时直接定位。
func (s *Service) notifyComment(ctx context.Context, recipient, actorID, postID uuid.UUID, notifyType string) {
	if s.notifier == nil {
		return
	}
	input := NotifyInput{
		UserID:     recipient,
		Type:       notifyType,
		ActorID:    actorID,
		TargetType: "post",
		TargetID:   postID,
	}
	if notifyType == "reply" {
		input.TargetType = "comment"
		input.Payload = map[string]any{"postId": postID.String()}
	}
	if err := s.notifier.Notify(ctx, input); err != nil {
		slog.Error("生成评论通知失败", "postId", postID, "type", notifyType, "err", err)
	}
}

// notifyRepost 生成转发通知，发给原帖作者。
// 原帖作者就是转发者时直接跳过；notify 服务层也会兜底，这里提前返回省一次查询。
func (s *Service) notifyRepost(ctx context.Context, originalAuthor, actorID, repostPostID, originalPostID uuid.UUID) {
	if s.notifier == nil || originalAuthor == uuid.Nil || originalAuthor == actorID {
		return
	}
	if err := s.notifier.Notify(ctx, NotifyInput{
		UserID:     originalAuthor,
		Type:       "repost",
		ActorID:    actorID,
		TargetType: "post",
		TargetID:   originalPostID,
		Payload:    map[string]any{"repostId": repostPostID.String()},
	}); err != nil {
		slog.Error("生成转发通知失败", "postId", originalPostID, "err", err)
	}
}

// notifyMentions 解析正文里的 @handle 并给被提及者发通知。
//
// 批量解析（一次 IN 查询）而不是逐条查：一条正文可能提及多人，
// 逐条查询会随提及数量线性增加数据库往返。
// 作者自己永远不收自己的提及通知；已通过其他渠道（回复/评论）通知过的人
// 仍会再收到一条 mention —— 两者语义不同，不应互相吞掉。
func (s *Service) notifyMentions(ctx context.Context, actorID uuid.UUID, targetType string, targetID uuid.UUID, text string, extra map[string]any) {
	if s.notifier == nil || s.handles == nil {
		return
	}
	handles := ExtractMentions(text)
	if len(handles) == 0 {
		return
	}
	resolved, err := s.handles.ResolveHandles(ctx, handles)
	if err != nil {
		// 解析失败只影响提及通知，不影响已经成功的发帖/评论。
		slog.Error("解析 @提及失败", "targetType", targetType, "targetId", targetID, "err", err)
		return
	}
	for _, handle := range handles {
		userID, ok := resolved[handle]
		if !ok || userID == uuid.Nil || userID == actorID {
			continue
		}
		payload := map[string]any{"handle": handle}
		for key, value := range extra {
			payload[key] = value
		}
		if err := s.notifier.Notify(ctx, NotifyInput{
			UserID:     userID,
			Type:       "mention",
			ActorID:    actorID,
			TargetType: targetType,
			TargetID:   targetID,
			Payload:    payload,
		}); err != nil {
			slog.Error("生成提及通知失败", "targetType", targetType, "targetId", targetID, "handle", handle, "err", err)
		}
	}
}

// ListComments 返回帖子的评论列表。
//
// userID 为 uuid.Nil 表示匿名访问。先做的可读性校验复用了与写路径相同的可见性
// 谓词（GetPostForWrite）：它的条件是「本人 / public / 已关注作者的 followers /
// 群成员」，传入 NULL 查看者时只剩 public 成立，因此匿名只能看到 public 帖的评论，
// 私密帖与不存在的帖子一样返回 ErrPostNotFound。
func (s *Service) ListComments(ctx context.Context, userID, postID uuid.UUID, rawCursor string, limit int32) (CommentsResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	createdAt, id, _, err := DecodeCursor(rawCursor, "comments")
	if err != nil {
		return CommentsResult{}, err
	}
	if _, err := s.queries.GetPostForWrite(ctx, db.GetPostForWriteParams{ID: uuidParam(postID), AuthorID: uuidParam(userID)}); errors.Is(err, pgx.ErrNoRows) {
		return CommentsResult{}, ErrPostNotFound
	} else if err != nil {
		return CommentsResult{}, err
	}
	timeCursor := pgtype.Timestamptz{Time: createdAt, Valid: rawCursor != ""}
	rows, err := s.queries.ListComments(ctx, db.ListCommentsParams{PostID: uuidParam(postID), Column2: timeCursor, Column3: uuidParam(id), Limit: limit + 1})
	if err != nil {
		return CommentsResult{}, err
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	comments := make([]Comment, 0, len(rows))
	for _, row := range rows {
		comments = append(comments, toComment(db.GetCommentByIDRow(row)))
	}
	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := EncodeCursor("comments", last.CreatedAt.Time, last.ID.Bytes, nil)
		if err != nil {
			return CommentsResult{}, err
		}
		next = &encoded
	}
	return CommentsResult{Comments: comments, NextCursor: next}, nil
}

func toComment(row db.GetCommentByIDRow) Comment {
	return Comment{ID: row.ID.Bytes, PostID: row.PostID.Bytes, ParentID: uuidPointer(row.ParentID), Author: CommentAuthor{ID: row.AuthorID.Bytes, Name: row.AuthorName, Handle: row.AuthorHandle}, ContentHTML: row.ContentHtml, ContentText: row.ContentText, LikeCount: row.LikeCount, CreatedAt: row.CreatedAt.Time.UTC()}
}

func postTags(value any) ([]string, error) {
	switch raw := value.(type) {
	case []string:
		return raw, nil
	case []any:
		tags := make([]string, len(raw))
		for i, tag := range raw {
			text, ok := tag.(string)
			if !ok {
				return nil, fmt.Errorf("标签投影元素类型不正确: %T", tag)
			}
			tags[i] = text
		}
		return tags, nil
	default:
		return nil, fmt.Errorf("标签投影类型不正确: %T", value)
	}
}

// PostFromRow 把帖子行投影转换为 Post DTO。
//
// 导出它是为了让群组模块复用同一份投影：群内动态的 SQL 列与 GetPostByID
// 完全一致，直接调用即可保证两个入口的 PostDTO 永不走样。
func PostFromRow(row db.GetPostByIDRow) (Post, error) { return toPost(row) }

func toPost(row db.GetPostByIDRow) (Post, error) {
	tags, err := postTags(row.Tags)
	if err != nil {
		return Post{}, err
	}
	post := Post{ID: row.ID.Bytes, Type: string(row.Type), Author: Author{ID: row.AuthorID.Bytes, Name: row.AuthorName, Handle: row.AuthorHandle, VerifyType: string(row.AuthorVerifyType)}, ContentHTML: stringValue(row.ContentHtml), ContentText: stringValue(row.ContentText), GroupID: uuidPointer(row.GroupID), Visibility: string(row.Visibility), LikeCount: row.LikeCount, CommentCount: row.CommentCount, RepostCount: row.RepostCount, HotScore: row.HotScore, CreatedAt: row.CreatedAt.Time.UTC(), Liked: row.Liked, Bookmarked: row.Bookmarked, Tags: tags}
	if row.PaperIDValue.Valid {
		post.Paper, err = makePaper(row.PaperIDValue, row.PaperDoi, row.PaperArxivID, row.PaperOpenreviewID, row.PaperTitle, row.PaperAuthors, row.PaperAbstract, row.PaperVenue, row.PaperPublishedAt, row.PaperCitationCount, row.PaperMetaSource, row.PaperCreatedAt)
		if err != nil {
			return Post{}, err
		}
	}
	if row.RepostID.Valid {
		var repostPaper *paper.Paper
		if row.RepostPaperID.Valid {
			repostPaper, err = makePaper(row.RepostPaperID, row.RepostPaperDoi, row.RepostPaperArxivID, row.RepostPaperOpenreviewID, row.RepostPaperTitle, row.RepostPaperAuthors, row.RepostPaperAbstract, row.RepostPaperVenue, row.RepostPaperPublishedAt, row.RepostPaperCitationCount, row.RepostPaperMetaSource, row.RepostPaperCreatedAt)
			if err != nil {
				return Post{}, err
			}
		}
		post.RepostOf = &RepostSummary{ID: row.RepostID.Bytes, AuthorName: stringValue(row.RepostAuthorName), ContentText: stringValue(row.RepostContentText), Paper: repostPaper}
	}
	return post, nil
}

// attachmentQuerier 是附件查询的最小依赖。
// 单独声明而不是直接用 *db.Queries，是为了让单元测试能注入替身。
type attachmentQuerier interface {
	ListAttachmentsForPosts(context.Context, []pgtype.UUID) ([]db.ListAttachmentsForPostsRow, error)
}

// attachFiles 为一批帖子补上附件。
//
// 一条 IN 查询覆盖整页：每张卡片单独查一次会变成 N+1，
// 而信息流一页 20 条，代价不可忽略。
func (s *Service) attachFiles(ctx context.Context, q attachmentQuerier, posts []Post) error {
	if len(posts) == 0 {
		return nil
	}
	// 测试替身场景没有附件查询能力：直接按「无附件」处理。
	if q == nil {
		for i := range posts {
			posts[i].Attachments = []Attachment{}
		}
		return nil
	}
	ids := make([]pgtype.UUID, 0, len(posts))
	for _, post := range posts {
		ids = append(ids, uuidParam(post.ID))
	}
	rows, err := q.ListAttachmentsForPosts(ctx, ids)
	if err != nil {
		return err
	}
	byPost := make(map[uuid.UUID][]Attachment, len(posts))
	for _, row := range rows {
		postID := uuid.UUID(row.PostID.Bytes)
		pageCount := int32(0)
		if row.PageCount != nil {
			pageCount = *row.PageCount
		}
		byPost[postID] = append(byPost[postID], Attachment{
			FileID:    uuid.UUID(row.ID.Bytes),
			Name:      row.OriginalName,
			Mime:      row.Mime,
			Kind:      row.Kind,
			SizeBytes: row.SizeBytes,
			PageCount: pageCount,
		})
	}
	for i := range posts {
		if files, ok := byPost[posts[i].ID]; ok {
			posts[i].Attachments = files
		} else {
			posts[i].Attachments = []Attachment{}
		}
	}
	return nil
}

func makePaper(id pgtype.UUID, doi, arxivID, openreviewID, title *string, authorsJSON []byte, abstract, venue *string, published pgtype.Date, citation *int32, source *string, created pgtype.Timestamptz) (*paper.Paper, error) {
	var authors []paper.Author
	if len(authorsJSON) > 0 {
		if err := json.Unmarshal(authorsJSON, &authors); err != nil {
			return nil, fmt.Errorf("解析论文作者失败: %w", err)
		}
	}
	if authors == nil {
		authors = []paper.Author{}
	}
	year, count := 0, 0
	if published.Valid {
		year = published.Time.Year()
	}
	if citation != nil {
		count = int(*citation)
	}
	return &paper.Paper{ID: id.Bytes, DOI: stringValue(doi), ArxivID: stringValue(arxivID), OpenreviewID: stringValue(openreviewID), Title: stringValue(title), Authors: authors, Abstract: stringValue(abstract), Venue: stringValue(venue), PublishedYear: year, CitationCount: count, MetaSource: stringValue(source), CreatedAt: created.Time.UTC()}, nil
}

// ---------------------------------------------------------------------------
// 帖子编辑与删除
// ---------------------------------------------------------------------------

// roleAdmin 与 users.role 枚举中的运营角色一致。
// 单独定义而不复用 curation 包的常量：content 不应为了一个字符串反向依赖 curation。
const roleAdmin = "admin"

// UpdatePostInput 是编辑帖子的局部更新入参。
// 指针为 nil 表示请求体没有提供该字段，因此不修改它——这与 PATCH 的语义一致。
type UpdatePostInput struct {
	Content    *string
	Visibility *string
}

// indexRemover 是检索索引的「删除」能力。
//
// 不直接把它塞进 Indexer 接口：Indexer 的既有实现（server 包的 contentIndexer）
// 只实现了 Index，改接口会让 server 包立刻编译失败。用类型断言探测能力，
// 索引实现补上 Remove 后即自动生效，未补时删除帖子也不会报错。
type indexRemover interface {
	Remove(ctx context.Context, entityType string, entityID uuid.UUID) error
}

// CanModifyPost 判定用户能否编辑或删除某条帖子。
//
// 规则：作者本人，或平台运营（role='admin'，用于处置违规内容）。
// 抽成不依赖数据库的纯函数，便于单元测试覆盖「非作者非运营」这一关键分支。
func CanModifyPost(actorID, authorID uuid.UUID, actorRole string) bool {
	return actorID == authorID || actorRole == roleAdmin
}

// actorRole 查询操作者的平台角色；查询失败返回空字符串（按普通用户处理）。
//
// 只在「操作者不是作者」时才需要这一次查询：作者本人无需知道角色。
// 查询失败按非运营处理——宁可拒绝一次越权编辑，也不能因为查不到角色而放行。
func (s *Service) actorRole(ctx context.Context, actorID uuid.UUID) string {
	if s.pool == nil {
		return ""
	}
	user, err := db.New(s.pool).GetUserByID(ctx, uuidParam(actorID))
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("查询操作者角色失败", "userId", actorID, "err", err)
		}
		return ""
	}
	return string(user.Role)
}

// canModify 是 UpdatePost / DeletePost 共用的权限闸门。
//
// 作者本人无需查库；只有「非作者」才去查角色，省掉绝大多数请求的一次查询。
func (s *Service) canModify(ctx context.Context, actorID, authorID uuid.UUID) bool {
	if actorID == authorID {
		return true
	}
	return CanModifyPost(actorID, authorID, s.actorRole(ctx, actorID))
}

// UpdatePost 编辑帖子的正文与可见性。
//
// 可改字段只有 content / visibility。type、paper_id、repost_of、group_id
// 不允许改：它们决定「这条帖子是什么」，改动等于换了一条内容，应删旧发新。
//
// 权限语义：帖子不存在返回 ErrPostNotFound（404）；存在但非作者非运营返回
// ErrForbidden（403）。帖子是公开的，隐藏存在性没有意义。
func (s *Service) UpdatePost(ctx context.Context, actorID, postID uuid.UUID, input UpdatePostInput) (Post, error) {
	if input.Content == nil && input.Visibility == nil {
		return Post{}, fmt.Errorf("%w: 没有需要更新的字段", ErrValidation)
	}
	q := db.New(s.pool)
	row, err := q.GetPostAuthorAndType(ctx, uuidParam(postID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrPostNotFound
	}
	if err != nil {
		return Post{}, err
	}
	authorID := uuid.UUID(row.AuthorID.Bytes)
	if !s.canModify(ctx, actorID, authorID) {
		return Post{}, ErrForbidden
	}

	params := db.UpdatePostParams{ID: uuidParam(postID)}
	if input.Content != nil {
		content := strings.TrimSpace(*input.Content)
		if utf8.RuneCountInString(content) > 5000 {
			return Post{}, fmt.Errorf("%w: 正文不能超过 5000 字符", ErrValidation)
		}
		// 转发可以只转发不附言，原创帖不能把正文清空——那会变成一条空内容。
		// 查询把 type 投影成 ::text，因此生成类型是 string，直接比较即可。
		if content == "" && row.Type != string(db.PostTypeRepost) {
			return Post{}, fmt.Errorf("%w: 正文不能为空", ErrValidation)
		}
		htmlText, plainText := SanitizeContent(content)
		params.ContentHtml = &htmlText
		params.ContentText = &plainText
	}
	if input.Visibility != nil {
		visibility := *input.Visibility
		switch db.PostVisibility(visibility) {
		case db.PostVisibilityPublic, db.PostVisibilityFollowers, db.PostVisibilityGroup, db.PostVisibilityPrivate:
		default:
			return Post{}, fmt.Errorf("%w: 可见性不正确", ErrValidation)
		}
		// 群内帖只能保持 group：改成 public 会把群内容泄露到全站，
		// 改成 followers / private 又会让群成员看不到，两种都不合理。
		// 非群帖也不能改成 group（没有目标群）。
		if row.GroupID.Valid && visibility != string(db.PostVisibilityGroup) {
			return Post{}, fmt.Errorf("%w: 群内帖的可见性必须保持为 group", ErrValidation)
		}
		if !row.GroupID.Valid && visibility == string(db.PostVisibilityGroup) {
			return Post{}, fmt.Errorf("%w: 非群内帖不能设为 group", ErrValidation)
		}
		params.Visibility = db.NullPostVisibility{PostVisibility: db.PostVisibility(visibility), Valid: true}
	}

	if _, err := q.UpdatePost(ctx, params); err != nil {
		return Post{}, err
	}

	// 用作者身份取回 DTO：运营编辑他人私密帖时，以运营身份读会被可见性谓词挡住，
	// 而作者一定读得到自己的帖子。权限校验已在上面完成，这里只是回显。
	// 先取回再同步索引，可以直接用更新后的可见性与正文，无需自行推导。
	post, err := s.GetPost(ctx, authorID, postID)
	if err != nil {
		return Post{}, err
	}

	// 索引与可见性联动，以「更新后的实际可见性」为准：
	//   - 结果是 public 且有正文 → 写入/刷新索引；
	//   - 结果不是 public（转私密/群内）→ 移出索引，避免匿名检索读到受限正文。
	// 只按 input 判断是不够的：只改正文、可见性原本就是 private 的帖子
	// 也会被误写进索引。索引失败只记日志——编辑已经成功，不应表现为 500。
	if post.Visibility == string(db.PostVisibilityPublic) {
		s.indexPost(ctx, postID, post.Visibility, post.ContentText)
	} else {
		s.removeFromIndex(ctx, postID)
	}
	return post, nil
}

// DeletePost 软删除帖子。
//
// 软删除保留行用于审计与恢复；所有读路径都带 deleted_at IS NULL，因此立即不可见。
// 删除转发不会影响原帖（只更新本行的 deleted_at）。
func (s *Service) DeletePost(ctx context.Context, actorID, postID uuid.UUID) error {
	q := db.New(s.pool)
	row, err := q.GetPostAuthorAndType(ctx, uuidParam(postID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPostNotFound
	}
	if err != nil {
		return err
	}
	authorID := uuid.UUID(row.AuthorID.Bytes)
	if !s.canModify(ctx, actorID, authorID) {
		return ErrForbidden
	}
	affected, err := q.SoftDeletePost(ctx, uuidParam(postID))
	if err != nil {
		return err
	}
	if affected == 0 {
		// 并发删除：检查与更新之间已被别人删掉，按「已不存在」处理。
		return ErrPostNotFound
	}
	s.removeFromIndex(ctx, postID)
	return nil
}

// removeFromIndex 把帖子移出检索索引。索引实现不支持 Remove 时静默跳过，
// 由后续的重建任务清理残留文档（与 indexPost 的降级策略一致）。
func (s *Service) removeFromIndex(ctx context.Context, postID uuid.UUID) {
	if s.indexer == nil {
		return
	}
	remover, ok := s.indexer.(indexRemover)
	if !ok {
		return
	}
	if err := remover.Remove(ctx, "post", postID); err != nil {
		slog.Error("从索引移除帖子失败", "postId", postID, "err", err)
	}
}
