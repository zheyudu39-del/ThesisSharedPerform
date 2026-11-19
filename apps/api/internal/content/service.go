package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
}

type FeedResult struct {
	Posts      []Post  `json:"posts"`
	NextCursor *string `json:"nextCursor"`
}

type CommentsResult struct {
	Comments   []Comment `json:"comments"`
	NextCursor *string   `json:"nextCursor"`
}

// Service 实现内容领域逻辑；pgx 连接池天然支持多请求并发。
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

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
	for _, tag := range tags {
		record, err := q.UpsertTag(ctx, db.UpsertTagParams{Name: tag.name, Slug: tag.slug})
		if err != nil {
			return Post{}, err
		}
		if err := q.LinkPostTag(ctx, db.LinkPostTagParams{PostID: created.ID, TagID: record.ID}); err != nil {
			return Post{}, err
		}
	}
	if input.RepostOf != nil {
		if err := q.IncrementRepostCount(ctx, uuidParam(*input.RepostOf)); err != nil {
			return Post{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Post{}, err
	}
	return s.GetPost(ctx, authorID, created.ID.Bytes)
}

func (s *Service) GetPost(ctx context.Context, userID, postID uuid.UUID) (Post, error) {
	row, err := db.New(s.pool).GetPostByID(ctx, db.GetPostByIDParams{AuthorID: uuidParam(postID), UserID: uuidParam(userID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrPostNotFound
	}
	if err != nil {
		return Post{}, err
	}
	return toPost(row)
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
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return count, nil
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
	if err := tx.Commit(ctx); err != nil {
		return Comment{}, err
	}
	return toComment(row), nil
}

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
	q := db.New(s.pool)
	if _, err := q.GetPostForWrite(ctx, db.GetPostForWriteParams{ID: uuidParam(postID), AuthorID: uuidParam(userID)}); errors.Is(err, pgx.ErrNoRows) {
		return CommentsResult{}, ErrPostNotFound
	} else if err != nil {
		return CommentsResult{}, err
	}
	timeCursor := pgtype.Timestamptz{Time: createdAt, Valid: rawCursor != ""}
	rows, err := q.ListComments(ctx, db.ListCommentsParams{PostID: uuidParam(postID), Column2: timeCursor, Column3: uuidParam(id), Limit: limit + 1})
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
