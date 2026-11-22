package content

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thesis-shared/api/internal/db"
)

// 话题标签（PRD F-07）。
//
// 标签本身早就存在——发帖时可以带 tags，帖子卡片也会展示它们——
// 但没有任何页面能按标签浏览，标签在界面上只是一段不可点击的文字。

// Tag 是一个话题标签及其公开帖子数。
type Tag struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	PostCount int64  `json:"postCount"`
}

// GetTag 按 slug 查询标签详情。不存在时返回 ErrPostNotFound
// （与帖子/用户一致：不存在与无权访问共用 404，避免探测）。
func (s *Service) GetTag(ctx context.Context, slug string) (Tag, error) {
	normalized, err := normalizeTagSlug(slug)
	if err != nil {
		return Tag{}, err
	}
	row, err := db.New(s.pool).GetTagBySlug(ctx, normalized)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Tag{}, ErrPostNotFound
		}
		return Tag{}, fmt.Errorf("查询标签失败: %w", err)
	}
	return Tag{Name: row.Name, Slug: row.Slug, PostCount: row.PostCount}, nil
}

// TagPosts 返回某个标签下的公开帖子，游标分页。
func (s *Service) TagPosts(ctx context.Context, viewerID uuid.UUID, slug, rawCursor string, limit int32) (FeedResult, error) {
	normalized, err := normalizeTagSlug(slug)
	if err != nil {
		return FeedResult{}, err
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	createdAt, id, _, err := DecodeCursor(rawCursor, "tag")
	if err != nil {
		return FeedResult{}, err
	}

	q := db.New(s.pool)
	rows, err := q.ListPostsByTag(ctx, db.ListPostsByTagParams{
		ViewerID:   uuidParam(viewerID),
		Slug:       normalized,
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
		slog.Error("加载话题帖子附件失败", "tag", normalized, "err", err)
	}

	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := EncodeCursor("tag", last.CreatedAt.Time, last.ID.Bytes, nil)
		if err != nil {
			return FeedResult{}, err
		}
		next = &encoded
	}
	return FeedResult{Posts: posts, NextCursor: next}, nil
}

// TopTags 返回按公开帖子数排序的热门标签。
func (s *Service) TopTags(ctx context.Context, limit int32) ([]Tag, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := db.New(s.pool).ListTopTags(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("查询热门标签失败: %w", err)
	}
	tags := make([]Tag, 0, len(rows))
	for _, row := range rows {
		tags = append(tags, Tag{Name: row.Name, Slug: row.Slug, PostCount: row.PostCount})
	}
	return tags, nil
}

// normalizeTagSlug 校验并归一化标签 slug。
//
// 与写入时保持一致（见 content.service 的标签处理）：小写、去空白。
// 长度上限与 tags.slug 列定义一致。
func normalizeTagSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if slug == "" {
		return "", fmt.Errorf("%w: 标签不能为空", ErrValidation)
	}
	if len(slug) > 64 {
		return "", fmt.Errorf("%w: 标签不能超过 64 个字符", ErrValidation)
	}
	return slug, nil
}
