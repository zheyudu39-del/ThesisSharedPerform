package curation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/content"
	"github.com/thesis-shared/api/internal/db"
	"github.com/thesis-shared/api/internal/paper"
)

// 字段长度上限，与迁移里的列定义一致。
const (
	maxTitleLen       = 128
	maxDescriptionLen = 500
	maxNoteLen        = 280
	maxSlugLen        = 64
)

// Collection 是策展集对外 DTO。
type Collection struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	// CoverURL 恒为 null：封面对象存储链路尚未接入，先保留字段占位。
	CoverURL  *string   `json:"coverUrl"`
	Curator   CuratorRef `json:"curator"`
	ItemCount int32     `json:"itemCount"`
	StartsAt  time.Time `json:"startsAt"`
	EndsAt    *time.Time `json:"endsAt"`
	CreatedAt time.Time  `json:"createdAt"`
	// CanEdit 让前端直接决定是否渲染编辑入口，省去一次权限探测。
	CanEdit bool `json:"canEdit"`
}

// CuratorRef 是策展人摘要。
type CuratorRef struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Handle     string    `json:"handle"`
	VerifyType string    `json:"verifyType"`
}

// Item 是策展项 DTO。
//
// Post 与 Paper 恰好有一个非 nil —— 迁移里的 CHECK 约束保证了这一点。
// 前端按哪个非空决定渲染帖子卡片还是论文卡片。
type Item struct {
	ID       uuid.UUID   `json:"id"`
	Position int32       `json:"position"`
	Note     string      `json:"note"`
	Post     *content.Post `json:"post"`
	Paper    *paper.Paper  `json:"paper"`
	AddedBy  CuratorRef    `json:"addedBy"`
	CreatedAt time.Time    `json:"createdAt"`
}

// CollectionsResult 是策展集列表的一页。
type CollectionsResult struct {
	Collections []Collection `json:"collections"`
	NextCursor  *string      `json:"nextCursor"`
}

// CollectionDetail 是策展集详情：元数据 + 条目。
type CollectionDetail struct {
	Collection Collection `json:"collection"`
	Items      []Item     `json:"items"`
}

// CreateCollectionInput 是建集入参。
type CreateCollectionInput struct {
	Title       string
	Description string
	// StartsAt / EndsAt 均可选；零值表示使用数据库默认（立即上架 / 长期有效）。
	StartsAt *time.Time
	EndsAt   *time.Time
}

// UpdateCollectionInput 是局部更新入参：nil 表示请求体未提供该字段。
type UpdateCollectionInput struct {
	Title       *string
	Description *string
	StartsAt    *time.Time
	EndsAt      *time.Time
}

// AddItemInput 是添加策展项入参。PostID 与 PaperID 恰好提供一个。
type AddItemInput struct {
	PostID   *uuid.UUID
	PaperID  *uuid.UUID
	Note     string
	Position *int32
}

// Querier 限定策展服务使用的生成查询，便于单元测试注入替身。
type Querier interface {
	CreateCollection(context.Context, db.CreateCollectionParams) (db.Collection, error)
	CollectionSlugExists(context.Context, string) (bool, error)
	GetCollectionBySlug(context.Context, string) (db.GetCollectionBySlugRow, error)
	GetCollectionByID(context.Context, pgtype.UUID) (db.GetCollectionByIDRow, error)
	UpdateCollection(context.Context, db.UpdateCollectionParams) (db.Collection, error)
	SoftDeleteCollection(context.Context, pgtype.UUID) error
	ListActiveCollections(context.Context, db.ListActiveCollectionsParams) ([]db.ListActiveCollectionsRow, error)

	AddCollectionItem(context.Context, db.AddCollectionItemParams) (db.CollectionItem, error)
	ListCollectionItems(context.Context, pgtype.UUID) ([]db.ListCollectionItemsRow, error)
	GetCollectionItem(context.Context, db.GetCollectionItemParams) (db.CollectionItem, error)
	RemoveCollectionItem(context.Context, db.RemoveCollectionItemParams) error
	ReorderCollectionItem(context.Context, db.ReorderCollectionItemParams) error
	RecomputeCollectionItemCount(context.Context, pgtype.UUID) (int32, error)

	GetUserByID(context.Context, pgtype.UUID) (db.User, error)
}

// Service 提供策展集与策展项的读写。
type Service struct {
	pool    *pgxpool.Pool
	queries Querier
}

// NewService 构造策展服务。
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, queries: db.New(pool)}
}

// ---------------------------------------------------------------------------
// 策展集
// ---------------------------------------------------------------------------

// CreateCollection 新建策展集。仅运营与大V 可调用（PRD §3）。
func (s *Service) CreateCollection(ctx context.Context, userID uuid.UUID, input CreateCollectionInput) (Collection, error) {
	if userID == uuid.Nil {
		return Collection{}, ErrForbidden
	}
	title, err := requireText(input.Title, maxTitleLen, "标题")
	if err != nil {
		return Collection{}, err
	}
	description, err := optionalText(input.Description, maxDescriptionLen, "简介")
	if err != nil {
		return Collection{}, err
	}
	if err := validateWindow(input.StartsAt, input.EndsAt); err != nil {
		return Collection{}, err
	}

	user, err := s.loadCurator(ctx, userID)
	if err != nil {
		return Collection{}, err
	}
	if !CanCurate(user) {
		return Collection{}, ErrNotCurator
	}

	slug, err := s.uniqueSlug(ctx, Slugify(title))
	if err != nil {
		return Collection{}, err
	}

	startsAt := pgtype.Timestamptz{}
	if input.StartsAt != nil {
		startsAt = pgtype.Timestamptz{Time: input.StartsAt.UTC(), Valid: true}
	}
	endsAt := pgtype.Timestamptz{}
	if input.EndsAt != nil {
		endsAt = pgtype.Timestamptz{Time: input.EndsAt.UTC(), Valid: true}
	}

	created, err := s.queries.CreateCollection(ctx, db.CreateCollectionParams{
		Slug:        slug,
		Title:       title,
		Description: optionalString(description),
		CuratorID:   uuidParam(userID),
		Visibility:  db.PostVisibilityPublic,
		Column7:     startsAt,
		EndsAt:      endsAt,
	})
	if err != nil {
		return Collection{}, fmt.Errorf("创建策展集失败: %w", err)
	}

	return Collection{
		ID:          fromPG(created.ID),
		Slug:        created.Slug,
		Title:       created.Title,
		Description: deref(created.Description),
		Curator: CuratorRef{
			ID:         user.ID,
			Name:       user.Name,
			Handle:     user.Handle,
			VerifyType: user.VerifyType,
		},
		ItemCount: created.ItemCount,
		StartsAt:  fromPGTime(created.StartsAt),
		EndsAt:    fromPGTimePtr(created.EndsAt),
		CreatedAt: fromPGTime(created.CreatedAt),
		CanEdit:   true,
	}, nil
}

// GetCollection 返回策展集详情与条目。viewerID 为 uuid.Nil 表示匿名。
//
// 未上架（starts_at 在未来）或已下架（ends_at 已过）的集合对非策展人
// 一律视为不存在，避免通过 slug 探测尚未发布的选题。
func (s *Service) GetCollection(ctx context.Context, viewerID uuid.UUID, slug string) (CollectionDetail, error) {
	row, err := s.queries.GetCollectionBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return CollectionDetail{}, ErrCollectionNotFound
	}
	if err != nil {
		return CollectionDetail{}, err
	}

	viewer, err := s.loadViewer(ctx, viewerID)
	if err != nil {
		return CollectionDetail{}, err
	}
	canEdit := CanEdit(viewer, fromPG(row.CuratorID))

	// 非策展人只能看到「已上架且未下架」的公开集合。
	if row.Visibility != db.PostVisibilityPublic {
		if !canEdit {
			return CollectionDetail{}, ErrCollectionNotFound
		}
	}
	if !canEdit && !isLive(row.StartsAt, row.EndsAt) {
		return CollectionDetail{}, ErrCollectionNotFound
	}

	items, err := s.ListItems(ctx, viewerID, fromPG(row.ID))
	if err != nil {
		return CollectionDetail{}, err
	}

	return CollectionDetail{
		Collection: toCollection(fieldsFromSlugRow(row), canEdit),
		Items:      items,
	}, nil
}

// UpdateCollection 局部更新策展集。仅策展人本人或运营。
func (s *Service) UpdateCollection(ctx context.Context, userID uuid.UUID, slug string, input UpdateCollectionInput) (Collection, error) {
	row, viewer, err := s.loadEditable(ctx, userID, slug)
	if err != nil {
		return Collection{}, err
	}

	params := db.UpdateCollectionParams{ID: row.ID}
	if input.Title != nil {
		title, err := requireText(*input.Title, maxTitleLen, "标题")
		if err != nil {
			return Collection{}, err
		}
		params.Title = &title
	}
	if input.Description != nil {
		desc, err := optionalText(*input.Description, maxDescriptionLen, "简介")
		if err != nil {
			return Collection{}, err
		}
		params.Description = optionalString(desc)
	}
	if input.StartsAt != nil {
		params.StartsAt = pgtype.Timestamptz{Time: input.StartsAt.UTC(), Valid: true}
	}
	if input.EndsAt != nil {
		params.EndsAt = pgtype.Timestamptz{Time: input.EndsAt.UTC(), Valid: true}
	}

	updated, err := s.queries.UpdateCollection(ctx, params)
	if err != nil {
		return Collection{}, fmt.Errorf("更新策展集失败: %w", err)
	}

	// 更新后重新读一次带策展人摘要的行，保证响应与列表接口形状一致。
	fresh, err := s.queries.GetCollectionByID(ctx, updated.ID)
	if err != nil {
		return Collection{}, fmt.Errorf("读取策展集失败: %w", err)
	}
	return toCollection(fieldsFromIDRow(fresh), CanEdit(viewer, fromPG(fresh.CuratorID))), nil
}

// DeleteCollection 软删除策展集。仅策展人本人或运营。
func (s *Service) DeleteCollection(ctx context.Context, userID uuid.UUID, slug string) error {
	row, _, err := s.loadEditable(ctx, userID, slug)
	if err != nil {
		return err
	}
	if err := s.queries.SoftDeleteCollection(ctx, row.ID); err != nil {
		return fmt.Errorf("删除策展集失败: %w", err)
	}
	return nil
}

// ListCollections 返回一页在架策展集，多取一条用于判断是否有下一页。
func (s *Service) ListCollections(ctx context.Context, viewerID uuid.UUID, rawCursor string, limit int32) (CollectionsResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	startsAt, id, err := decodeCursor(rawCursor, cursorStreamCollections)
	if err != nil {
		return CollectionsResult{}, err
	}

	viewer, err := s.loadViewer(ctx, viewerID)
	if err != nil {
		return CollectionsResult{}, err
	}

	params := db.ListActiveCollectionsParams{
		CursorTime: pgtype.Timestamptz{Time: startsAt, Valid: rawCursor != ""},
		CursorID:   uuidParam(id),
		Limit:      limit + 1,
	}
	rows, err := s.queries.ListActiveCollections(ctx, params)
	if err != nil {
		return CollectionsResult{}, err
	}

	var next *string
	if int32(len(rows)) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		cursor, err := encodeCursor(cursorStreamCollections, fromPGTime(last.StartsAt), fromPG(last.ID))
		if err != nil {
			return CollectionsResult{}, err
		}
		next = &cursor
	}

	collections := make([]Collection, 0, len(rows))
	for _, row := range rows {
		collections = append(collections, toCollection(fieldsFromListRow(row), CanEdit(viewer, fromPG(row.CuratorID))))
	}
	return CollectionsResult{Collections: collections, NextCursor: next}, nil
}

// ---------------------------------------------------------------------------
// 策展项
// ---------------------------------------------------------------------------

// AddItem 往集合里加一条内容。仅策展人本人或运营。
func (s *Service) AddItem(ctx context.Context, userID uuid.UUID, slug string, input AddItemInput) (Item, error) {
	row, _, err := s.loadEditable(ctx, userID, slug)
	if err != nil {
		return Item{}, err
	}

	// 必须且只能指定帖子或论文之一 —— 与迁移里的 CHECK 约束对应。
	if (input.PostID == nil) == (input.PaperID == nil) {
		return Item{}, fmt.Errorf("%w: 必须且只能指定帖子或论文之一", ErrValidation)
	}
	note, err := optionalText(input.Note, maxNoteLen, "推荐语")
	if err != nil {
		return Item{}, err
	}
	position := int32(0)
	if input.Position != nil {
		if *input.Position < 0 {
			return Item{}, fmt.Errorf("%w: 排序值不能为负", ErrValidation)
		}
		position = *input.Position
	}

	created, err := s.queries.AddCollectionItem(ctx, db.AddCollectionItemParams{
		CollectionID: row.ID,
		PostID:       optionalUUID(input.PostID),
		PaperID:      optionalUUID(input.PaperID),
		Note:         optionalString(note),
		Position:     position,
		AddedBy:      uuidParam(userID),
	})
	if err != nil {
		return Item{}, mapItemInsertError(err)
	}

	if _, err := s.queries.RecomputeCollectionItemCount(ctx, row.ID); err != nil {
		// 计数只是展示用的冗余字段，重算失败不应让整个请求失败。
		slog.Error("重算策展项数量失败", "collectionId", fromPG(row.ID), "err", err)
	}

	items, err := s.ListItems(ctx, userID, fromPG(row.ID))
	if err != nil {
		return Item{}, err
	}
	for _, item := range items {
		if item.ID == fromPG(created.ID) {
			return item, nil
		}
	}
	return Item{}, fmt.Errorf("策展项写入后未能读回")
}

// RemoveItem 从集合里移除一条内容。
func (s *Service) RemoveItem(ctx context.Context, userID uuid.UUID, slug string, itemID uuid.UUID) error {
	row, _, err := s.loadEditable(ctx, userID, slug)
	if err != nil {
		return err
	}
	if _, err := s.queries.GetCollectionItem(ctx, db.GetCollectionItemParams{
		ID: uuidParam(itemID), CollectionID: row.ID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return ErrItemNotFound
	} else if err != nil {
		return err
	}
	if err := s.queries.RemoveCollectionItem(ctx, db.RemoveCollectionItemParams{
		ID: uuidParam(itemID), CollectionID: row.ID,
	}); err != nil {
		return fmt.Errorf("移除策展项失败: %w", err)
	}
	if _, err := s.queries.RecomputeCollectionItemCount(ctx, row.ID); err != nil {
		slog.Error("重算策展项数量失败", "collectionId", fromPG(row.ID), "err", err)
	}
	return nil
}

// ReorderItem 调整策展项顺序。
func (s *Service) ReorderItem(ctx context.Context, userID uuid.UUID, slug string, itemID uuid.UUID, position int32) error {
	if position < 0 {
		return fmt.Errorf("%w: 排序值不能为负", ErrValidation)
	}
	row, _, err := s.loadEditable(ctx, userID, slug)
	if err != nil {
		return err
	}
	if _, err := s.queries.GetCollectionItem(ctx, db.GetCollectionItemParams{
		ID: uuidParam(itemID), CollectionID: row.ID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return ErrItemNotFound
	} else if err != nil {
		return err
	}
	if err := s.queries.ReorderCollectionItem(ctx, db.ReorderCollectionItemParams{
		ID: uuidParam(itemID), CollectionID: row.ID, Position: position,
	}); err != nil {
		return fmt.Errorf("调整策展项顺序失败: %w", err)
	}
	return nil
}

// ListItems 返回集合内全部条目，附带帖子或论文详情。
//
// 帖子走 content 服务的可见性谓词：策展集里如果混进了一条后来被设为
// 私密的帖子，匿名访客不应看到它的内容。这里对每条帖子单独取一次详情，
// 不可见的直接跳过。策展集条目数量在几十条量级，逐个查询可以接受；
// 若日后集合变大，应改成一条带可见性谓词的 JOIN。
func (s *Service) ListItems(ctx context.Context, viewerID uuid.UUID, collectionID uuid.UUID) ([]Item, error) {
	rows, err := s.queries.ListCollectionItems(ctx, uuidParam(collectionID))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []Item{}, nil
	}

	posts := content.NewService(s.pool)
	out := make([]Item, 0, len(rows))
	for _, row := range rows {
		item := Item{
			ID:        fromPG(row.ID),
			Position:  row.Position,
			Note:      deref(row.Note),
			AddedBy: CuratorRef{
				Name:   row.AddedByName,
				Handle: row.AddedByHandle,
			},
			CreatedAt: fromPGTime(row.CreatedAt),
		}

		if row.PostID.Valid {
			postID := fromPG(row.PostID)
			post, err := posts.GetPost(ctx, viewerID, postID)
			if errors.Is(err, content.ErrPostNotFound) {
				// 帖子已被删除或已对当前查看者不可见：跳过，不泄露存在性。
				continue
			}
			if err != nil {
				return nil, err
			}
			item.Post = &post
		} else if row.PaperID.Valid {
			paperSvc := paper.NewService(s.pool, paper.NewFetcher())
			p, err := paperSvc.GetByID(ctx, fromPG(row.PaperID))
			if errors.Is(err, paper.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			item.Paper = &p
		} else {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 内部辅助
// ---------------------------------------------------------------------------

// loadCurator 读取当前账号的身份，用于策展资格判定。
func (s *Service) loadCurator(ctx context.Context, userID uuid.UUID) (Curator, error) {
	user, err := s.queries.GetUserByID(ctx, uuidParam(userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Curator{}, ErrForbidden
	}
	if err != nil {
		return Curator{}, err
	}
	return Curator{
		ID:         fromPG(user.ID),
		Name:       user.Name,
		Handle:     user.Handle,
		Role:       string(user.Role),
		VerifyType: string(user.VerifyType),
	}, nil
}

// loadViewer 读取查看者身份。匿名返回零值 Curator，不报错。
func (s *Service) loadViewer(ctx context.Context, userID uuid.UUID) (Curator, error) {
	if userID == uuid.Nil {
		return Curator{}, nil
	}
	user, err := s.queries.GetUserByID(ctx, uuidParam(userID))
	if errors.Is(err, pgx.ErrNoRows) {
		// 会话有效但账号已删除/封禁：按匿名处理，而不是让公开读接口报错。
		return Curator{}, nil
	}
	if err != nil {
		return Curator{}, err
	}
	return Curator{
		ID:         fromPG(user.ID),
		Name:       user.Name,
		Handle:     user.Handle,
		Role:       string(user.Role),
		VerifyType: string(user.VerifyType),
	}, nil
}

// loadEditable 读取集合并校验编辑权限，返回集合行与查看者身份。
func (s *Service) loadEditable(ctx context.Context, userID uuid.UUID, slug string) (db.GetCollectionBySlugRow, Curator, error) {
	if userID == uuid.Nil {
		return db.GetCollectionBySlugRow{}, Curator{}, ErrForbidden
	}
	row, err := s.queries.GetCollectionBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.GetCollectionBySlugRow{}, Curator{}, ErrCollectionNotFound
	}
	if err != nil {
		return db.GetCollectionBySlugRow{}, Curator{}, err
	}
	viewer, err := s.loadViewer(ctx, userID)
	if err != nil {
		return db.GetCollectionBySlugRow{}, Curator{}, err
	}
	if !CanEdit(viewer, fromPG(row.CuratorID)) {
		return db.GetCollectionBySlugRow{}, Curator{}, ErrForbidden
	}
	return row, viewer, nil
}

// uniqueSlug 在 exists 判定下生成唯一 slug：先试基础形式，冲突则追加随机后缀。
func (s *Service) uniqueSlug(ctx context.Context, base string) (string, error) {
	if base == "" {
		base = "collection"
	}
	if len(base) > maxSlugLen {
		base = strings.TrimRight(base[:maxSlugLen], "-")
	}
	exists := func(candidate string) (bool, error) {
		return s.queries.CollectionSlugExists(ctx, candidate)
	}
	return uniqueSlug(base, exists, randomSuffix)
}

// isLive 判断集合当前是否在架（已上架且未下架）。
func isLive(startsAt, endsAt pgtype.Timestamptz) bool {
	now := time.Now().UTC()
	if startsAt.Valid && startsAt.Time.After(now) {
		return false
	}
	if endsAt.Valid && !endsAt.Time.After(now) {
		return false
	}
	return true
}

// validateWindow 校验时间窗口：结束时间必须晚于开始时间。
func validateWindow(startsAt, endsAt *time.Time) error {
	if startsAt != nil && endsAt != nil && !endsAt.After(*startsAt) {
		return fmt.Errorf("%w: 结束时间必须晚于开始时间", ErrValidation)
	}
	return nil
}

// requireText 校验必填文本：去空白后非空且不超过上限。
func requireText(raw string, max int, field string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("%w: %s不能为空", ErrValidation, field)
	}
	if utf8.RuneCountInString(value) > max {
		return "", fmt.Errorf("%w: %s不能超过 %d 个字符", ErrValidation, field, max)
	}
	return value, nil
}

// optionalText 校验可选文本：允许为空，但非空时不得超过上限。
func optionalText(raw string, max int, field string) (string, error) {
	value := strings.TrimSpace(raw)
	if utf8.RuneCountInString(value) > max {
		return "", fmt.Errorf("%w: %s不能超过 %d 个字符", ErrValidation, field, max)
	}
	return value, nil
}

// mapItemInsertError 把数据库约束冲突翻译成领域错误。
func mapItemInsertError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation：同一条内容重复加入
			return ErrDuplicateItem
		case "23503": // foreign_key_violation：引用的帖子/论文不存在
			return ErrTargetNotFound
		case "23514": // check_violation：post_id 与 paper_id 的互斥约束
			return fmt.Errorf("%w: 必须且只能指定帖子或论文之一", ErrValidation)
		}
	}
	return fmt.Errorf("添加策展项失败: %w", err)
}

// collectionFields 是三种查询行（BySlug / ByID / List）共有的字段。
//
// sqlc 为每条查询生成独立的行类型，字段完全一致但不共享类型。
// 用这个中间结构承载映射逻辑，避免写三份几乎相同的转换函数——
// 三份拷贝意味着将来加字段时必定漏改其中一份。
type collectionFields struct {
	ID                pgtype.UUID
	Slug              string
	Title             string
	Description       *string
	CuratorID         pgtype.UUID
	StartsAt          pgtype.Timestamptz
	EndsAt            pgtype.Timestamptz
	ItemCount         int32
	CreatedAt         pgtype.Timestamptz
	CuratorName       string
	CuratorHandle     string
	CuratorVerifyType db.VerifyType
}

// toCollection 把共有字段转换为 DTO。
func toCollection(f collectionFields, canEdit bool) Collection {
	return Collection{
		ID:          fromPG(f.ID),
		Slug:        f.Slug,
		Title:       f.Title,
		Description: deref(f.Description),
		Curator: CuratorRef{
			ID:         fromPG(f.CuratorID),
			Name:       f.CuratorName,
			Handle:     f.CuratorHandle,
			VerifyType: string(f.CuratorVerifyType),
		},
		ItemCount: f.ItemCount,
		StartsAt:  fromPGTime(f.StartsAt),
		EndsAt:    fromPGTimePtr(f.EndsAt),
		CreatedAt: fromPGTime(f.CreatedAt),
		CanEdit:   canEdit,
	}
}

// fieldsFromSlugRow / fieldsFromIDRow / fieldsFromListRow 把三种行类型
// 投影到共有字段。
func fieldsFromSlugRow(row db.GetCollectionBySlugRow) collectionFields {
	return collectionFields{
		ID: row.ID, Slug: row.Slug, Title: row.Title, Description: row.Description,
		CuratorID: row.CuratorID, StartsAt: row.StartsAt, EndsAt: row.EndsAt,
		ItemCount: row.ItemCount, CreatedAt: row.CreatedAt,
		CuratorName: row.CuratorName, CuratorHandle: row.CuratorHandle,
		CuratorVerifyType: row.CuratorVerifyType,
	}
}

func fieldsFromIDRow(row db.GetCollectionByIDRow) collectionFields {
	return collectionFields{
		ID: row.ID, Slug: row.Slug, Title: row.Title, Description: row.Description,
		CuratorID: row.CuratorID, StartsAt: row.StartsAt, EndsAt: row.EndsAt,
		ItemCount: row.ItemCount, CreatedAt: row.CreatedAt,
		CuratorName: row.CuratorName, CuratorHandle: row.CuratorHandle,
		CuratorVerifyType: row.CuratorVerifyType,
	}
}

func fieldsFromListRow(row db.ListActiveCollectionsRow) collectionFields {
	return collectionFields{
		ID: row.ID, Slug: row.Slug, Title: row.Title, Description: row.Description,
		CuratorID: row.CuratorID, StartsAt: row.StartsAt, EndsAt: row.EndsAt,
		ItemCount: row.ItemCount, CreatedAt: row.CreatedAt,
		CuratorName: row.CuratorName, CuratorHandle: row.CuratorHandle,
		CuratorVerifyType: row.CuratorVerifyType,
	}
}

func uuidParam(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }

func optionalUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return uuidParam(*id)
}

func fromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}

func fromPGTime(ts pgtype.Timestamptz) time.Time {
	if !ts.Valid {
		return time.Time{}
	}
	return ts.Time
}

func fromPGTimePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
