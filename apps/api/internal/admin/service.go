// Package admin 实现运营后台的数据统计与内容管理。
//
// 与 moderation 的分工：
//   - moderation 负责举报与屏蔽，是「用户举报 → 运营处理」的闭环；
//   - admin 负责全局视角：概览统计、用户与内容管理、文件排查。
//
// 权限：所有方法都以 actorID 为第一个参数，内部调用 requireAdmin 二次校验。
// 路由层只保证「已登录」，角色判定始终在服务层——与 moderation 一致，
// 避免新增接口时漏挂中间件就变成越权入口。
package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thesis-shared/api/internal/db"
)

var (
	// ErrValidation 表示入参不合法（分页、筛选值、搜索词）。
	ErrValidation = errors.New("输入校验失败")
	// ErrForbidden 表示调用者不是运营。
	ErrForbidden = errors.New("没有权限执行该操作")
	// ErrNotFound 表示目标记录不存在或已被删除。
	ErrNotFound = errors.New("记录不存在")
	// ErrInvalidCursor 表示分页游标损坏或不属于当前列表。
	ErrInvalidCursor = errors.New("无效的分页游标")
	// ErrLastAdmin 表示该操作会让平台失去最后一名可用管理员。
	ErrLastAdmin = errors.New("不能移除最后一名管理员")
)

// 用户状态白名单。数据库里 status 是 varchar 而非枚举，
// 取值必须在服务层收敛，否则一个拼写错误就能造出「谁也登不上」的账号。
const (
	UserStatusActive    = "active"
	UserStatusSuspended = "suspended"
)

const (
	defaultLimit = 20
	maxLimit     = 100
	// maxActivityDays 限制趋势查询的窗口，避免一次拉出几年的日粒度数据。
	maxActivityDays = 90
)

// Querier 限定后台服务使用的查询，便于单元测试注入替身。
type Querier interface {
	AdminOverviewTotals(context.Context) (db.AdminOverviewTotalsRow, error)
	AdminDailyActivity(context.Context, int32) ([]db.AdminDailyActivityRow, error)
	AdminTopTags(context.Context, int32) ([]db.AdminTopTagsRow, error)

	AdminListUsers(context.Context, db.AdminListUsersParams) ([]db.AdminListUsersRow, error)
	AdminCountUsers(context.Context) (int64, error)
	AdminSetUserRole(context.Context, db.AdminSetUserRoleParams) (db.AdminSetUserRoleRow, error)
	AdminSetUserStatus(context.Context, db.AdminSetUserStatusParams) (db.AdminSetUserStatusRow, error)
	AdminCountAdmins(context.Context) (int64, error)

	AdminListPosts(context.Context, db.AdminListPostsParams) ([]db.AdminListPostsRow, error)
	AdminSoftDeletePost(context.Context, pgtype.UUID) (db.AdminSoftDeletePostRow, error)
	AdminRestorePost(context.Context, pgtype.UUID) (db.AdminRestorePostRow, error)

	AdminListPapers(context.Context, db.AdminListPapersParams) ([]db.AdminListPapersRow, error)

	AdminListComments(context.Context, db.AdminListCommentsParams) ([]db.AdminListCommentsRow, error)
	AdminSoftDeleteComment(context.Context, pgtype.UUID) (pgtype.UUID, error)
	AdminRestoreComment(context.Context, pgtype.UUID) (pgtype.UUID, error)

	AdminListGroups(context.Context, db.AdminListGroupsParams) ([]db.AdminListGroupsRow, error)
	AdminSoftDeleteGroup(context.Context, pgtype.UUID) (db.AdminSoftDeleteGroupRow, error)
	AdminRestoreGroup(context.Context, pgtype.UUID) (db.AdminRestoreGroupRow, error)

	AdminListFiles(context.Context, db.AdminListFilesParams) ([]db.AdminListFilesRow, error)
	AdminFileStats(context.Context) ([]db.AdminFileStatsRow, error)

	IsAdmin(context.Context, uuid.UUID) (bool, error)
}

// Service 实现后台领域逻辑。
type Service struct {
	pool    *pgxpool.Pool
	queries Querier
	now     func() time.Time
}

// NewService 构造服务。pool 为 nil 时只使用注入的替身（单元测试场景）。
func NewService(pool *pgxpool.Pool) *Service {
	s := &Service{pool: pool, now: time.Now}
	if pool != nil {
		s.queries = &pgQuerier{q: db.New(pool)}
	}
	return s
}

// NewServiceWithQuerier 用于测试注入替身。
func NewServiceWithQuerier(q Querier) *Service {
	return &Service{queries: q, now: time.Now}
}

/* -------------------------------------------------------------------------- */
/* 概览                                                                       */
/* -------------------------------------------------------------------------- */

// Overview 是后台首页的统计数据。
type Overview struct {
	Totals    Totals          `json:"totals"`
	Activity  []DailyActivity `json:"activity"`
	TopTags   []TagStat       `json:"topTags"`
	GeneratedAt time.Time     `json:"generatedAt"`
}

// Totals 是各实体的总量快照。
type Totals struct {
	Users          int64 `json:"users"`
	ActiveUsers    int64 `json:"activeUsers"`
	Admins         int64 `json:"admins"`
	Posts          int64 `json:"posts"`
	PublicPosts    int64 `json:"publicPosts"`
	Comments       int64 `json:"comments"`
	Papers         int64 `json:"papers"`
	PapersWithPDF  int64 `json:"papersWithPdf"`
	Groups         int64 `json:"groups"`
	Files          int64 `json:"files"`
	FailedFiles    int64 `json:"failedFiles"`
	StorageBytes   int64 `json:"storageBytes"`
	Documents      int64 `json:"documents"`
	PendingReports int64 `json:"pendingReports"`
	Blocks         int64 `json:"blocks"`
}

// DailyActivity 是一天的新增量。
type DailyActivity struct {
	Day         string `json:"day"`
	NewUsers    int64  `json:"newUsers"`
	NewPosts    int64  `json:"newPosts"`
	NewPapers   int64  `json:"newPapers"`
	NewComments int64  `json:"newComments"`
}

// TagStat 是话题热度。
type TagStat struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	PostCount int64  `json:"postCount"`
}

// GetOverview 返回概览数据。days 是趋势窗口天数。
func (s *Service) GetOverview(ctx context.Context, actorID uuid.UUID, days int32) (Overview, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return Overview{}, err
	}
	if days <= 0 {
		days = 14
	}
	if days > maxActivityDays {
		return Overview{}, fmt.Errorf("%w: days 不能超过 %d", ErrValidation, maxActivityDays)
	}

	totals, err := s.queries.AdminOverviewTotals(ctx)
	if err != nil {
		return Overview{}, fmt.Errorf("统计总量: %w", err)
	}
	rows, err := s.queries.AdminDailyActivity(ctx, days)
	if err != nil {
		return Overview{}, fmt.Errorf("统计趋势: %w", err)
	}
	tags, err := s.queries.AdminTopTags(ctx, 10)
	if err != nil {
		return Overview{}, fmt.Errorf("统计话题: %w", err)
	}

	activity := make([]DailyActivity, 0, len(rows))
	for _, r := range rows {
		activity = append(activity, DailyActivity{
			Day:         r.Day.Time.Format("2006-01-02"),
			NewUsers:    r.NewUsers,
			NewPosts:    r.NewPosts,
			NewPapers:   r.NewPapers,
			NewComments: r.NewComments,
		})
	}
	topTags := make([]TagStat, 0, len(tags))
	for _, t := range tags {
		topTags = append(topTags, TagStat{Slug: t.Slug, Name: t.Name, PostCount: t.PostCount})
	}

	return Overview{
		Totals: Totals{
			Users:          totals.Users,
			ActiveUsers:    totals.ActiveUsers,
			Admins:         totals.Admins,
			Posts:          totals.Posts,
			PublicPosts:    totals.PublicPosts,
			Comments:       totals.Comments,
			Papers:         totals.Papers,
			PapersWithPDF:  totals.PapersWithPdf,
			Groups:         totals.Groups,
			Files:          totals.Files,
			FailedFiles:    totals.FailedFiles,
			StorageBytes:   totals.StorageBytes,
			Documents:      totals.Documents,
			PendingReports: totals.PendingReports,
			Blocks:         totals.Blocks,
		},
		Activity:    activity,
		TopTags:     topTags,
		GeneratedAt: s.now().UTC(),
	}, nil
}

/* -------------------------------------------------------------------------- */
/* 用户管理                                                                   */
/* -------------------------------------------------------------------------- */

// UserItem 是用户列表的一行。
//
// 刻意不返回 password_hash；email 返回给运营用于定位账号，
// 但前端只在列表里显示，不提供导出。
type UserItem struct {
	ID            uuid.UUID  `json:"id"`
	Email         string     `json:"email"`
	Name          string     `json:"name"`
	Handle        string     `json:"handle"`
	Role          string     `json:"role"`
	Status        string     `json:"status"`
	VerifyType    string     `json:"verifyType"`
	Institution   string     `json:"institution"`
	PostCount     int64      `json:"postCount"`
	FollowerCount int64      `json:"followerCount"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastActiveAt  *time.Time `json:"lastActiveAt"`
}

// UserPage 是用户列表的一页。
type UserPage struct {
	Users      []UserItem `json:"users"`
	NextCursor *string    `json:"nextCursor"`
	Total      int64      `json:"total"`
}

// UserFilter 是用户列表的筛选条件。
type UserFilter struct {
	Query  string
	Role   string
	Status string
	Cursor string
	Limit  int32
}

// ListUsers 返回用户列表。query 为模糊搜索词。
func (s *Service) ListUsers(ctx context.Context, actorID uuid.UUID, f UserFilter) (UserPage, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return UserPage{}, err
	}
	limit := normalizeLimit(f.Limit)

	var query *string
	if q := strings.TrimSpace(f.Query); q != "" {
		if len([]rune(q)) > 64 {
			return UserPage{}, fmt.Errorf("%w: 搜索词不能超过 64 个字符", ErrValidation)
		}
		// ILIKE 的通配符必须转义，否则用户搜 "100%" 会变成前缀匹配。
		escaped := escapeLike(q)
		pattern := "%" + escaped + "%"
		query = &pattern
	}

	var role db.NullUserRole
	if r := strings.TrimSpace(f.Role); r != "" {
		value := db.UserRole(r)
		if !value.Valid() {
			return UserPage{}, fmt.Errorf("%w: role 只能是 user 或 admin", ErrValidation)
		}
		role = db.NullUserRole{UserRole: value, Valid: true}
	}

	var status *string
	if st := strings.TrimSpace(f.Status); st != "" {
		if st != UserStatusActive && st != UserStatusSuspended {
			return UserPage{}, fmt.Errorf("%w: status 只能是 active 或 suspended", ErrValidation)
		}
		status = &st
	}

	cursorAt, cursorID, err := decodeCursor(f.Cursor, "users")
	if err != nil {
		return UserPage{}, err
	}

	rows, err := s.queries.AdminListUsers(ctx, db.AdminListUsersParams{
		Query:      query,
		Role:       role,
		Status:     status,
		CursorAt:   cursorAt,
		CursorID:   cursorID,
		LimitCount: limit,
	})
	if err != nil {
		return UserPage{}, fmt.Errorf("查询用户列表: %w", err)
	}
	total, err := s.queries.AdminCountUsers(ctx)
	if err != nil {
		return UserPage{}, fmt.Errorf("统计用户总数: %w", err)
	}

	users := make([]UserItem, 0, len(rows))
	for _, r := range rows {
		item := UserItem{
			ID:            uuid.UUID(r.ID.Bytes),
			Name:          r.Name,
			Handle:        r.Handle,
			Role:          string(r.Role),
			Status:        r.Status,
			VerifyType:    string(r.VerifyType),
			PostCount:     r.PostCount,
			FollowerCount: r.FollowerCount,
			CreatedAt:     r.CreatedAt.Time,
		}
		if r.Email != nil {
			item.Email = *r.Email
		}
		if r.Institution != nil {
			item.Institution = *r.Institution
		}
		if r.LastActiveAt.Valid {
			t := r.LastActiveAt.Time
			item.LastActiveAt = &t
		}
		users = append(users, item)
	}

	return UserPage{
		Users:      users,
		NextCursor: nextCursor(len(rows), limit, "users", rows[len(rows)-1].CreatedAt, rows[len(rows)-1].ID, len(rows) > 0),
		Total:      total,
	}, nil
}

// SetUserRole 修改用户角色。
//
// 降级最后一名管理员会被拒绝：否则后台再没有人能进，
// 只能去数据库里手工改回来。
func (s *Service) SetUserRole(ctx context.Context, actorID, targetID uuid.UUID, role string) (UserItem, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return UserItem{}, err
	}
	value := db.UserRole(role)
	if !value.Valid() {
		return UserItem{}, fmt.Errorf("%w: role 只能是 user 或 admin", ErrValidation)
	}
	if actorID == targetID && value != db.UserRoleAdmin {
		return UserItem{}, fmt.Errorf("%w: 不能撤销自己的管理员权限", ErrValidation)
	}
	if value != db.UserRoleAdmin {
		// 只有「降级管理员」才有失去最后一个管理员的风险。
		admins, err := s.queries.AdminCountAdmins(ctx)
		if err != nil {
			return UserItem{}, fmt.Errorf("统计管理员数: %w", err)
		}
		target, err := s.userRoleOf(ctx, targetID)
		if err != nil {
			return UserItem{}, err
		}
		if target == db.UserRoleAdmin && admins <= 1 {
			return UserItem{}, ErrLastAdmin
		}
	}

	row, err := s.queries.AdminSetUserRole(ctx, db.AdminSetUserRoleParams{
		Role: value,
		ID:   pgtype.UUID{Bytes: targetID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserItem{}, ErrNotFound
		}
		return UserItem{}, fmt.Errorf("修改角色: %w", err)
	}
	return UserItem{
		ID:     uuid.UUID(row.ID.Bytes),
		Handle: row.Handle,
		Role:   string(row.Role),
		Status: row.Status,
	}, nil
}

// SetUserStatus 封禁或解封用户。
//
// 封禁立即生效：identity.GetByID 会拒绝 status != 'active' 的账号，
// 现有会话在下一个请求就失效，不必等 JWT 过期。
func (s *Service) SetUserStatus(ctx context.Context, actorID, targetID uuid.UUID, status string) (UserItem, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return UserItem{}, err
	}
	if status != UserStatusActive && status != UserStatusSuspended {
		return UserItem{}, fmt.Errorf("%w: status 只能是 active 或 suspended", ErrValidation)
	}
	if actorID == targetID && status != UserStatusActive {
		return UserItem{}, fmt.Errorf("%w: 不能封禁自己", ErrValidation)
	}
	if status != UserStatusActive {
		admins, err := s.queries.AdminCountAdmins(ctx)
		if err != nil {
			return UserItem{}, fmt.Errorf("统计管理员数: %w", err)
		}
		target, err := s.userRoleOf(ctx, targetID)
		if err != nil {
			return UserItem{}, err
		}
		if target == db.UserRoleAdmin && admins <= 1 {
			return UserItem{}, ErrLastAdmin
		}
	}

	row, err := s.queries.AdminSetUserStatus(ctx, db.AdminSetUserStatusParams{
		Status: status,
		ID:     pgtype.UUID{Bytes: targetID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserItem{}, ErrNotFound
		}
		return UserItem{}, fmt.Errorf("修改状态: %w", err)
	}
	return UserItem{
		ID:     uuid.UUID(row.ID.Bytes),
		Handle: row.Handle,
		Role:   string(row.Role),
		Status: row.Status,
	}, nil
}

// userRoleOf 查目标用户的当前角色，用于「最后一名管理员」判定。
func (s *Service) userRoleOf(ctx context.Context, userID uuid.UUID) (db.UserRole, error) {
	page, err := s.queries.AdminListUsers(ctx, db.AdminListUsersParams{
		LimitCount: 1,
	})
	_ = page
	_ = err
	// 上面的通用列表无法按 id 过滤，这里直接查一次当前角色。
	// 为保持 Querier 接口最小，改用 IsAdmin 无法区分 user/admin 的
	// 「是否管理员」——足够判断风险：只要目标是管理员才需要计数保护。
	ok, err := s.queries.IsAdmin(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.UserRoleUser, ErrNotFound
		}
		return db.UserRoleUser, err
	}
	if ok {
		return db.UserRoleAdmin, nil
	}
	return db.UserRoleUser, nil
}

/* -------------------------------------------------------------------------- */
/* 内容管理                                                                   */
/* -------------------------------------------------------------------------- */

// PostItem 是内容管理里的一行帖子。
type PostItem struct {
	ID           uuid.UUID  `json:"id"`
	AuthorID     uuid.UUID  `json:"authorId"`
	AuthorHandle string     `json:"authorHandle"`
	AuthorName   string     `json:"authorName"`
	Type         string     `json:"type"`
	Visibility   string     `json:"visibility"`
	GroupID      *uuid.UUID `json:"groupId"`
	Excerpt      string     `json:"excerpt"`
	PaperID      *uuid.UUID `json:"paperId"`
	LikeCount    int32      `json:"likeCount"`
	CommentCount int32      `json:"commentCount"`
	RepostCount  int32      `json:"repostCount"`
	CreatedAt    time.Time  `json:"createdAt"`
	DeletedAt    *time.Time `json:"deletedAt"`
}

// ContentFilter 是内容列表的通用筛选。
type ContentFilter struct {
	Query          string
	Cursor         string
	Limit          int32
	IncludeDeleted bool
}

// PostPage 是帖子管理列表的一页。
type PostPage struct {
	Posts      []PostItem `json:"posts"`
	NextCursor *string    `json:"nextCursor"`
}

// ListPosts 返回帖子管理列表。
func (s *Service) ListPosts(ctx context.Context, actorID uuid.UUID, f ContentFilter) (PostPage, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return PostPage{}, err
	}
	limit := normalizeLimit(f.Limit)
	query, err := searchPattern(f.Query)
	if err != nil {
		return PostPage{}, err
	}
	cursorAt, cursorID, err := decodeCursor(f.Cursor, "posts")
	if err != nil {
		return PostPage{}, err
	}

	rows, err := s.queries.AdminListPosts(ctx, db.AdminListPostsParams{
		IncludeDeleted: boolParam(f.IncludeDeleted),
		Query:          query,
		CursorAt:       cursorAt,
		CursorID:       cursorID,
		LimitCount:     limit,
	})
	if err != nil {
		return PostPage{}, fmt.Errorf("查询帖子: %w", err)
	}

	posts := make([]PostItem, 0, len(rows))
	for _, r := range rows {
		item := PostItem{
			ID:           uuid.UUID(r.ID.Bytes),
			AuthorID:     uuid.UUID(r.AuthorID.Bytes),
			AuthorHandle: r.AuthorHandle,
			AuthorName:   r.AuthorName,
			Type:         string(r.Type),
			Visibility:   string(r.Visibility),
			Excerpt:      r.Excerpt,
			LikeCount:    r.LikeCount,
			CommentCount: r.CommentCount,
			RepostCount:  r.RepostCount,
			CreatedAt:    r.CreatedAt.Time,
		}
		if r.GroupID.Valid {
			id := uuid.UUID(r.GroupID.Bytes)
			item.GroupID = &id
		}
		if r.PaperID.Valid {
			id := uuid.UUID(r.PaperID.Bytes)
			item.PaperID = &id
		}
		if r.DeletedAt.Valid {
			t := r.DeletedAt.Time
			item.DeletedAt = &t
		}
		posts = append(posts, item)
	}

	var next *string
	if len(rows) == int(limit) {
		last := rows[len(rows)-1]
		next = encodeCursor("posts", last.CreatedAt.Time, uuid.UUID(last.ID.Bytes))
	}
	return PostPage{Posts: posts, NextCursor: next}, nil
}

// DeletePost 软删除帖子。恢复用 RestorePost。
func (s *Service) DeletePost(ctx context.Context, actorID, postID uuid.UUID) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	_, err := s.queries.AdminSoftDeletePost(ctx, pgtype.UUID{Bytes: postID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("删除帖子: %w", err)
	}
	return nil
}

// RestorePost 恢复被软删除的帖子。
func (s *Service) RestorePost(ctx context.Context, actorID, postID uuid.UUID) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	_, err := s.queries.AdminRestorePost(ctx, pgtype.UUID{Bytes: postID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("恢复帖子: %w", err)
	}
	return nil
}

// PaperItem 是论文管理列表的一行。
type PaperItem struct {
	ID            uuid.UUID  `json:"id"`
	Title         string     `json:"title"`
	Venue         string     `json:"venue"`
	PublishedAt   *time.Time `json:"publishedAt"`
	CitationCount int32      `json:"citationCount"`
	MetaSource    string     `json:"metaSource"`
	PDFFileID     *uuid.UUID `json:"pdfFileId"`
	AuthorCount   int32      `json:"authorCount"`
	PostCount     int64      `json:"postCount"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// PaperPage 是论文管理列表的一页。
type PaperPage struct {
	Papers     []PaperItem `json:"papers"`
	NextCursor *string     `json:"nextCursor"`
}

// ListPapers 返回论文管理列表。
func (s *Service) ListPapers(ctx context.Context, actorID uuid.UUID, f ContentFilter) (PaperPage, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return PaperPage{}, err
	}
	limit := normalizeLimit(f.Limit)
	query, err := searchPattern(f.Query)
	if err != nil {
		return PaperPage{}, err
	}
	cursorAt, cursorID, err := decodeCursor(f.Cursor, "papers")
	if err != nil {
		return PaperPage{}, err
	}

	rows, err := s.queries.AdminListPapers(ctx, db.AdminListPapersParams{
		Query:      query,
		CursorAt:   cursorAt,
		CursorID:   cursorID,
		LimitCount: limit,
	})
	if err != nil {
		return PaperPage{}, fmt.Errorf("查询论文: %w", err)
	}

	papers := make([]PaperItem, 0, len(rows))
	for _, r := range rows {
		item := PaperItem{
			ID:            uuid.UUID(r.ID.Bytes),
			Title:         r.Title,
			CitationCount: r.CitationCount,
			AuthorCount:   r.AuthorCount,
			PostCount:     r.PostCount,
			CreatedAt:     r.CreatedAt.Time,
		}
		if r.Venue != nil {
			item.Venue = *r.Venue
		}
		if r.MetaSource != nil {
			item.MetaSource = *r.MetaSource
		}
		if r.PublishedAt.Valid {
			t := r.PublishedAt.Time
			item.PublishedAt = &t
		}
		if r.PdfFileID.Valid {
			id := uuid.UUID(r.PdfFileID.Bytes)
			item.PDFFileID = &id
		}
		papers = append(papers, item)
	}

	var next *string
	if len(rows) == int(limit) {
		last := rows[len(rows)-1]
		next = encodeCursor("papers", last.CreatedAt.Time, uuid.UUID(last.ID.Bytes))
	}
	return PaperPage{Papers: papers, NextCursor: next}, nil
}

// CommentItem 是评论管理列表的一行。
type CommentItem struct {
	ID             uuid.UUID  `json:"id"`
	PostID         uuid.UUID  `json:"postId"`
	ParentID       *uuid.UUID `json:"parentId"`
	AuthorID       uuid.UUID  `json:"authorId"`
	AuthorHandle   string     `json:"authorHandle"`
	AuthorName     string     `json:"authorName"`
	Excerpt        string     `json:"excerpt"`
	LikeCount      int32      `json:"likeCount"`
	CreatedAt      time.Time  `json:"createdAt"`
	DeletedAt      *time.Time `json:"deletedAt"`
	PostVisibility string     `json:"postVisibility"`
	// PostDeleted 标记所属帖子是否已删除：帖子没了，单条评论的「查看」会 404。
	PostDeleted bool `json:"postDeleted"`
}

// CommentPage 是评论管理列表的一页。
type CommentPage struct {
	Comments   []CommentItem `json:"comments"`
	NextCursor *string       `json:"nextCursor"`
}

// ListComments 返回评论管理列表。
func (s *Service) ListComments(ctx context.Context, actorID uuid.UUID, f ContentFilter) (CommentPage, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return CommentPage{}, err
	}
	limit := normalizeLimit(f.Limit)
	query, err := searchPattern(f.Query)
	if err != nil {
		return CommentPage{}, err
	}
	cursorAt, cursorID, err := decodeCursor(f.Cursor, "comments")
	if err != nil {
		return CommentPage{}, err
	}

	rows, err := s.queries.AdminListComments(ctx, db.AdminListCommentsParams{
		IncludeDeleted: boolParam(f.IncludeDeleted),
		Query:          query,
		CursorAt:       cursorAt,
		CursorID:       cursorID,
		LimitCount:     limit,
	})
	if err != nil {
		return CommentPage{}, fmt.Errorf("查询评论: %w", err)
	}

	comments := make([]CommentItem, 0, len(rows))
	for _, r := range rows {
		item := CommentItem{
			ID:             uuid.UUID(r.ID.Bytes),
			PostID:         uuid.UUID(r.PostID.Bytes),
			AuthorID:       uuid.UUID(r.AuthorID.Bytes),
			AuthorHandle:   r.AuthorHandle,
			AuthorName:     r.AuthorName,
			Excerpt:        r.Excerpt,
			LikeCount:      r.LikeCount,
			CreatedAt:      r.CreatedAt.Time,
			PostVisibility: string(r.PostVisibility),
			PostDeleted:    r.PostDeletedAt.Valid,
		}
		if r.ParentID.Valid {
			id := uuid.UUID(r.ParentID.Bytes)
			item.ParentID = &id
		}
		if r.DeletedAt.Valid {
			t := r.DeletedAt.Time
			item.DeletedAt = &t
		}
		comments = append(comments, item)
	}

	var next *string
	if len(rows) == int(limit) {
		last := rows[len(rows)-1]
		next = encodeCursor("comments", last.CreatedAt.Time, uuid.UUID(last.ID.Bytes))
	}
	return CommentPage{Comments: comments, NextCursor: next}, nil
}

// DeleteComment 软删除评论，并同步减少所属帖子的 comment_count。
func (s *Service) DeleteComment(ctx context.Context, actorID, commentID uuid.UUID) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	_, err := s.queries.AdminSoftDeleteComment(ctx, pgtype.UUID{Bytes: commentID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("删除评论: %w", err)
	}
	return nil
}

// RestoreComment 恢复被软删除的评论，并补回计数。
func (s *Service) RestoreComment(ctx context.Context, actorID, commentID uuid.UUID) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	_, err := s.queries.AdminRestoreComment(ctx, pgtype.UUID{Bytes: commentID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("恢复评论: %w", err)
	}
	return nil
}

// GroupItem 是群组管理列表的一行。
type GroupItem struct {
	ID            uuid.UUID  `json:"id"`
	Slug          string     `json:"slug"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	OwnerHandle   string     `json:"ownerHandle"`
	OwnerName     string     `json:"ownerName"`
	JoinPolicy    string     `json:"joinPolicy"`
	MemberCount   int32      `json:"memberCount"`
	DocumentCount int64      `json:"documentCount"`
	CreatedAt     time.Time  `json:"createdAt"`
	DeletedAt     *time.Time `json:"deletedAt"`
}

// GroupPage 是群组管理列表的一页。
type GroupPage struct {
	Groups     []GroupItem `json:"groups"`
	NextCursor *string     `json:"nextCursor"`
}

// ListGroups 返回群组管理列表。
func (s *Service) ListGroups(ctx context.Context, actorID uuid.UUID, f ContentFilter) (GroupPage, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return GroupPage{}, err
	}
	limit := normalizeLimit(f.Limit)
	query, err := searchPattern(f.Query)
	if err != nil {
		return GroupPage{}, err
	}
	cursorAt, cursorID, err := decodeCursor(f.Cursor, "groups")
	if err != nil {
		return GroupPage{}, err
	}

	rows, err := s.queries.AdminListGroups(ctx, db.AdminListGroupsParams{
		IncludeDeleted: boolParam(f.IncludeDeleted),
		Query:          query,
		CursorAt:       cursorAt,
		CursorID:       cursorID,
		LimitCount:     limit,
	})
	if err != nil {
		return GroupPage{}, fmt.Errorf("查询群组: %w", err)
	}

	groups := make([]GroupItem, 0, len(rows))
	for _, r := range rows {
		item := GroupItem{
			ID:            uuid.UUID(r.ID.Bytes),
			Name:          r.Name,
			OwnerHandle:   r.OwnerHandle,
			OwnerName:     r.OwnerName,
			JoinPolicy:    string(r.JoinPolicy),
			MemberCount:   r.MemberCount,
			DocumentCount: r.DocumentCount,
			CreatedAt:     r.CreatedAt.Time,
		}
		if r.Slug != nil {
			item.Slug = *r.Slug
		}
		if r.Description != nil {
			item.Description = *r.Description
		}
		if r.DeletedAt.Valid {
			t := r.DeletedAt.Time
			item.DeletedAt = &t
		}
		groups = append(groups, item)
	}

	var next *string
	if len(rows) == int(limit) {
		last := rows[len(rows)-1]
		next = encodeCursor("groups", last.CreatedAt.Time, uuid.UUID(last.ID.Bytes))
	}
	return GroupPage{Groups: groups, NextCursor: next}, nil
}

// DeleteGroup 软删除群组。
func (s *Service) DeleteGroup(ctx context.Context, actorID, groupID uuid.UUID) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	_, err := s.queries.AdminSoftDeleteGroup(ctx, pgtype.UUID{Bytes: groupID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("删除群组: %w", err)
	}
	return nil
}

// RestoreGroup 恢复被软删除的群组。
func (s *Service) RestoreGroup(ctx context.Context, actorID, groupID uuid.UUID) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	_, err := s.queries.AdminRestoreGroup(ctx, pgtype.UUID{Bytes: groupID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("恢复群组: %w", err)
	}
	return nil
}

/* -------------------------------------------------------------------------- */
/* 文件与存储                                                                 */
/* -------------------------------------------------------------------------- */

// FileItem 是文件排查列表的一行。
type FileItem struct {
	ID           uuid.UUID  `json:"id"`
	OwnerID      uuid.UUID  `json:"ownerId"`
	OwnerHandle  string     `json:"ownerHandle"`
	OwnerName    string     `json:"ownerName"`
	OriginalName string     `json:"originalName"`
	Kind         string     `json:"kind"`
	Mime         string     `json:"mime"`
	SizeBytes    int64      `json:"sizeBytes"`
	Status       string     `json:"status"`
	PageCount    int32      `json:"pageCount"`
	ErrorReason  string     `json:"errorReason"`
	RetryCount   int16      `json:"retryCount"`
	CreatedAt    time.Time  `json:"createdAt"`
	ReadyAt      *time.Time `json:"readyAt"`
}

// FilePage 是文件排查列表的一页。
type FilePage struct {
	Files      []FileItem  `json:"files"`
	NextCursor *string     `json:"nextCursor"`
	Stats      []FileStat  `json:"stats"`
}

// FileStat 是按状态汇总的文件统计。
type FileStat struct {
	Status    string `json:"status"`
	FileCount int64  `json:"fileCount"`
	Bytes     int64  `json:"bytes"`
}

// FileFilter 是文件列表的筛选条件。
type FileFilter struct {
	Status      string
	OnlyProblem bool
	Cursor      string
	Limit       int32
}

// ListFiles 返回文件排查列表，并附带按状态的汇总。
func (s *Service) ListFiles(ctx context.Context, actorID uuid.UUID, f FileFilter) (FilePage, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return FilePage{}, err
	}
	limit := normalizeLimit(f.Limit)

	var status db.NullFileStatus
	if raw := strings.TrimSpace(f.Status); raw != "" {
		value := db.FileStatus(raw)
		if !value.Valid() {
			return FilePage{}, fmt.Errorf("%w: 未知的文件状态 %q", ErrValidation, raw)
		}
		status = db.NullFileStatus{FileStatus: value, Valid: true}
	}
	cursorAt, cursorID, err := decodeCursor(f.Cursor, "files")
	if err != nil {
		return FilePage{}, err
	}

	rows, err := s.queries.AdminListFiles(ctx, db.AdminListFilesParams{
		Status:      status,
		OnlyProblem: boolParam(f.OnlyProblem),
		CursorAt:    cursorAt,
		CursorID:    cursorID,
		LimitCount:  limit,
	})
	if err != nil {
		return FilePage{}, fmt.Errorf("查询文件: %w", err)
	}
	statRows, err := s.queries.AdminFileStats(ctx)
	if err != nil {
		return FilePage{}, fmt.Errorf("统计文件: %w", err)
	}

	files := make([]FileItem, 0, len(rows))
	for _, r := range rows {
		item := FileItem{
			ID:           uuid.UUID(r.ID.Bytes),
			OwnerID:      uuid.UUID(r.OwnerID.Bytes),
			OwnerHandle:  r.OwnerHandle,
			OwnerName:    r.OwnerName,
			OriginalName: r.OriginalName,
			Kind:         string(r.Kind),
			Mime:         r.Mime,
			SizeBytes:    r.SizeBytes,
			Status:       string(r.Status),
			RetryCount:   r.RetryCount,
			CreatedAt:    r.CreatedAt.Time,
		}
		if r.PageCount != nil {
			item.PageCount = *r.PageCount
		}
		if r.ErrorReason != nil {
			item.ErrorReason = *r.ErrorReason
		}
		if r.ReadyAt.Valid {
			t := r.ReadyAt.Time
			item.ReadyAt = &t
		}
		files = append(files, item)
	}

	stats := make([]FileStat, 0, len(statRows))
	for _, r := range statRows {
		stats = append(stats, FileStat{
			Status:    string(r.Status),
			FileCount: r.FileCount,
			Bytes:     r.Bytes,
		})
	}

	var next *string
	if len(rows) == int(limit) {
		last := rows[len(rows)-1]
		next = encodeCursor("files", last.CreatedAt.Time, uuid.UUID(last.ID.Bytes))
	}
	return FilePage{Files: files, NextCursor: next, Stats: stats}, nil
}

/* -------------------------------------------------------------------------- */
/* 内部工具                                                                   */
/* -------------------------------------------------------------------------- */

// CheckAdmin 对外暴露运营身份校验，供其它服务（如文件重试）复用同一套规则。
//
// 单独开一个导出方法而不是让调用方自己查库：角色判定只应有一个实现，
// 否则「谁能进后台」会在多个包里各写一遍，早晚分叉。
func (s *Service) CheckAdmin(ctx context.Context, userID uuid.UUID) error {
	return s.requireAdmin(ctx, userID)
}

// requireAdmin 在服务层再查一次角色，不信任路由或前端。
func (s *Service) requireAdmin(ctx context.Context, userID uuid.UUID) error {
	if userID == uuid.Nil {
		return ErrForbidden
	}
	admin, err := s.queries.IsAdmin(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrForbidden
		}
		return fmt.Errorf("校验管理员身份: %w", err)
	}
	if !admin {
		return ErrForbidden
	}
	return nil
}

func normalizeLimit(limit int32) int32 {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

// escapeLike 转义 ILIKE 的通配符，避免用户输入被当成模式。
func escapeLike(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
}

// searchPattern 把搜索词变成 ILIKE 模式；空串返回 nil 表示不过滤。
func searchPattern(raw string) (*string, error) {
	q := strings.TrimSpace(raw)
	if q == "" {
		return nil, nil
	}
	if len([]rune(q)) > 64 {
		return nil, fmt.Errorf("%w: 搜索词不能超过 64 个字符", ErrValidation)
	}
	pattern := "%" + escapeLike(q) + "%"
	return &pattern, nil
}

func boolParam(v bool) *bool {
	if !v {
		return nil
	}
	return &v
}

// nextCursor 在取满一页时生成下一页游标。
func nextCursor(count int, limit int32, stream string, at pgtype.Timestamptz, id pgtype.UUID, ok bool) *string {
	if !ok || count < int(limit) {
		return nil
	}
	return encodeCursor(stream, at.Time, uuid.UUID(id.Bytes))
}
