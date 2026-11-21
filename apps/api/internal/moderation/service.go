package moderation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// 对外 DTO
// ---------------------------------------------------------------------------

// UserRef 是举报人与处理人的最小投影。
// 只带姓名与句柄：运营审核时不需要邮箱等私密字段，少查一列就少一份泄露面。
type UserRef struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Handle string    `json:"handle"`
}

// Report 是举报 DTO。
//
// Detail 为空时序列化为空串而不是 null：前端只需判断 truthy，
// 不必在「没有补充说明」与「字段缺失」之间做区分。
type Report struct {
	ID         uuid.UUID  `json:"id"`
	TargetType string     `json:"targetType"`
	TargetID   uuid.UUID  `json:"targetId"`
	Reason     string     `json:"reason"`
	Detail     string     `json:"detail"`
	Status     string     `json:"status"`
	Reporter   *UserRef   `json:"reporter"`
	HandledBy  *UserRef   `json:"handledBy"`
	HandledAt  *time.Time `json:"handledAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// ReportsResult 是举报列表的一页。
type ReportsResult struct {
	Reports    []Report `json:"reports"`
	NextCursor *string  `json:"nextCursor"`
}

// BlockedUser 是屏蔽列表里的一个用户。
type BlockedUser struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Handle     string    `json:"handle"`
	VerifyType string    `json:"verifyType"`
	BlockedAt  time.Time `json:"blockedAt"`
}

// BlocksResult 是屏蔽列表的一页。
type BlocksResult struct {
	Blocked    []BlockedUser `json:"blocked"`
	NextCursor *string       `json:"nextCursor"`
}

// CreateReportInput 是提交举报的入参。
type CreateReportInput struct {
	TargetType string
	TargetID   uuid.UUID
	Reason     string
	Detail     string
}

// 举报目标类型与原因枚举。与 PRD F-06 及前端下拉框严格对应；
// 用 map 而非字符串比较，新增取值时只需改这里一处。
var (
	validTargetTypes = map[string]struct{}{
		"post": {}, "comment": {}, "user": {}, "document": {},
	}
	validReasons = map[string]struct{}{
		"spam": {}, "harassment": {}, "misinformation": {}, "copyright": {}, "other": {},
	}
	validStatuses = map[string]struct{}{
		"reviewing": {}, "resolved": {}, "rejected": {},
	}
)

// ---------------------------------------------------------------------------
// 查询抽象
// ---------------------------------------------------------------------------

// CreateReportParams 是写入一条举报的参数。
type CreateReportParams struct {
	ReporterID uuid.UUID
	TargetType string
	TargetID   uuid.UUID
	Reason     string
	Detail     *string
}

// ReportRow 是举报列表 / 详情的联表投影。
type ReportRow struct {
	ID             uuid.UUID
	TargetType     string
	TargetID       uuid.UUID
	Reason         string
	Detail         *string
	Status         string
	ReporterID     uuid.UUID
	ReporterName   string
	ReporterHandle string
	HandlerID      pgtype.UUID
	HandlerName    *string
	HandlerHandle  *string
	HandledAt      pgtype.Timestamptz
	CreatedAt      time.Time
}

// ListReportsParams 是举报列表的查询参数。
// Status 为 nil 表示不筛选；CursorAt 无效表示第一页。
type ListReportsParams struct {
	Status   *string
	CursorAt pgtype.Timestamptz
	CursorID pgtype.UUID
	Limit    int32
}

// UpdateReportStatusParams 是修改举报状态的参数。
type UpdateReportStatusParams struct {
	ID        uuid.UUID
	Status    string
	HandledBy uuid.UUID
}

// TargetAuthorParams 用于解析举报目标的作者，判断是否「举报自己」。
type TargetAuthorParams struct {
	TargetType string
	TargetID   uuid.UUID
}

// BlockParams 是屏蔽 / 取消屏蔽的参数。
type BlockParams struct {
	BlockerID uuid.UUID
	BlockedID uuid.UUID
}

// ListBlocksParams 是屏蔽列表的查询参数。
type ListBlocksParams struct {
	BlockerID uuid.UUID
	CursorAt  pgtype.Timestamptz
	CursorID  pgtype.UUID
	Limit     int32
}

// BlockedUserRow 是屏蔽列表的联表投影。
type BlockedUserRow struct {
	UserID     uuid.UUID
	Name       string
	Handle     string
	VerifyType string
	CreatedAt  time.Time
}

// Querier 限定举报 / 屏蔽服务使用的查询，便于单元测试注入替身。
//
// 这里刻意不直接依赖 sqlc 生成的 db.*Params：moderation 的查询尚未生成代码，
// 先以本地类型描述契约，接线代理生成后按此签名对齐即可。
type Querier interface {
	LockReporter(context.Context, uuid.UUID) error
	ReportExists(context.Context, CreateReportParams) (bool, error)
	CreateReport(context.Context, CreateReportParams) (uuid.UUID, error)
	GetReportByID(context.Context, uuid.UUID) (ReportRow, error)
	ListReports(context.Context, ListReportsParams) ([]ReportRow, error)
	UpdateReportStatus(context.Context, UpdateReportStatusParams) (int64, error)

	IsAdmin(context.Context, uuid.UUID) (bool, error)
	GetUserIDByHandle(context.Context, string) (uuid.UUID, error)
	GetTargetAuthor(context.Context, TargetAuthorParams) (uuid.UUID, error)

	CreateBlock(context.Context, BlockParams) (int64, error)
	DeleteBlock(context.Context, BlockParams) (int64, error)
	ListBlocks(context.Context, ListBlocksParams) ([]BlockedUserRow, error)
	IsBlocked(context.Context, BlockParams) (bool, error)
}

// Service 实现举报与屏蔽领域逻辑。
type Service struct {
	pool    *pgxpool.Pool
	queries Querier
	now     func() time.Time
}

// NewService 构造服务。pool 为 nil 时读写都走注入的替身（单元测试场景）。
func NewService(pool *pgxpool.Pool) *Service {
	service := &Service{pool: pool, now: time.Now}
	if pool != nil {
		service.queries = newPGQuerier(pool)
	}
	return service
}

// newServiceWithQuerier 用于测试注入替身。
func newServiceWithQuerier(queries Querier) *Service {
	return &Service{queries: queries, now: time.Now}
}

// withTx 在事务中执行 fn；没有连接池（单元测试替身）时直接执行。
func (s *Service) withTx(ctx context.Context, fn func(Querier) error) error {
	if s.pool == nil {
		return fn(s.queries)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(newPGQuerier(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 举报
// ---------------------------------------------------------------------------

// CreateReport 提交一条举报。
//
// 顺序刻意是「校验目标 → 判自举报 → 写入」：
// 自举报与重复举报都必须在写库之前拦截，否则会先产生一行无意义的审核记录。
func (s *Service) CreateReport(ctx context.Context, reporterID uuid.UUID, input CreateReportInput) (Report, error) {
	targetType := strings.TrimSpace(input.TargetType)
	reason := strings.TrimSpace(input.Reason)
	detail := strings.TrimSpace(input.Detail)

	if _, ok := validTargetTypes[targetType]; !ok {
		return Report{}, fmt.Errorf("%w: targetType 只能是 post、comment、user 或 document", ErrValidation)
	}
	if input.TargetID == uuid.Nil {
		return Report{}, fmt.Errorf("%w: targetId 不能为空", ErrValidation)
	}
	if _, ok := validReasons[reason]; !ok {
		return Report{}, fmt.Errorf("%w: reason 只能是 spam、harassment、misinformation、copyright 或 other", ErrValidation)
	}
	// 数据库列是 varchar(500)；先按码点校验，避免把超长中文交给 PG 报错。
	if utf8.RuneCountInString(detail) > 500 {
		return Report{}, fmt.Errorf("%w: 补充说明不能超过 500 个字符", ErrValidation)
	}

	// 目标不存在时不能写入：否则审核队列里会出现无法定位的举报。
	authorID, err := s.queries.GetTargetAuthor(ctx, TargetAuthorParams{
		TargetType: targetType, TargetID: input.TargetID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, fmt.Errorf("%w: 举报目标不存在", ErrValidation)
	}
	if err != nil {
		return Report{}, fmt.Errorf("查询举报目标失败: %w", err)
	}
	if authorID == reporterID {
		return Report{}, ErrSelfReport
	}

	params := CreateReportParams{
		ReporterID: reporterID,
		TargetType: targetType,
		TargetID:   input.TargetID,
		Reason:     reason,
		Detail:     nullableString(detail),
	}

	var reportID uuid.UUID
	err = s.withTx(ctx, func(q Querier) error {
		// 先锁举报人行，再查重、再插入：reports 表没有 (reporter, target) 唯一索引，
		// 两个并发的相同举报若都先查后写，会各写一行。锁的粒度是单个举报人，
		// 不会阻塞其他用户。
		if err := q.LockReporter(ctx, reporterID); err != nil {
			return fmt.Errorf("锁定举报人失败: %w", err)
		}
		exists, err := q.ReportExists(ctx, params)
		if err != nil {
			return fmt.Errorf("查询举报记录失败: %w", err)
		}
		if exists {
			return ErrAlreadyReported
		}
		id, err := q.CreateReport(ctx, params)
		if err != nil {
			return fmt.Errorf("提交举报失败: %w", err)
		}
		reportID = id
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	return s.getReport(ctx, reportID)
}

// ListReports 列出举报，仅运营（role='admin'）可调用。
func (s *Service) ListReports(ctx context.Context, actorID uuid.UUID, status, rawCursor string, limit int32) (ReportsResult, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return ReportsResult{}, err
	}
	limit = normalizeLimit(limit)

	var statusFilter *string
	if raw := strings.TrimSpace(status); raw != "" {
		if _, ok := validReportStatuses()[raw]; !ok {
			return ReportsResult{}, fmt.Errorf("%w: status 只能是 pending、reviewing、resolved 或 rejected", ErrValidation)
		}
		statusFilter = &raw
	}

	createdAt, id, err := decodeCursor(rawCursor, "reports")
	if err != nil {
		return ReportsResult{}, err
	}
	rows, err := s.queries.ListReports(ctx, ListReportsParams{
		Status:   statusFilter,
		CursorAt: timeCursor(createdAt, rawCursor),
		CursorID: uuidParam(id),
		Limit:    limit + 1,
	})
	if err != nil {
		return ReportsResult{}, fmt.Errorf("查询举报列表失败: %w", err)
	}

	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	reports := make([]Report, 0, len(rows))
	for _, row := range rows {
		reports = append(reports, reportFromRow(row))
	}
	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := encodeCursor("reports", last.CreatedAt, last.ID)
		if err != nil {
			return ReportsResult{}, err
		}
		next = &encoded
	}
	return ReportsResult{Reports: reports, NextCursor: next}, nil
}

// UpdateReportStatus 修改举报状态，仅运营可调用。
//
// 允许的状态是 reviewing / resolved / rejected：pending 是「尚未处理」的初始值，
// 不应被运营手动设回，否则队列里会出现看似待处理、实际已有人看过的记录。
func (s *Service) UpdateReportStatus(ctx context.Context, actorID, reportID uuid.UUID, status string) (Report, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return Report{}, err
	}
	status = strings.TrimSpace(status)
	if _, ok := validStatuses[status]; !ok {
		return Report{}, fmt.Errorf("%w: status 只能是 reviewing、resolved 或 rejected", ErrValidation)
	}

	affected, err := s.queries.UpdateReportStatus(ctx, UpdateReportStatusParams{
		ID: reportID, Status: status, HandledBy: actorID,
	})
	if err != nil {
		return Report{}, fmt.Errorf("更新举报状态失败: %w", err)
	}
	if affected == 0 {
		return Report{}, ErrReportNotFound
	}
	return s.getReport(ctx, reportID)
}

func (s *Service) getReport(ctx context.Context, reportID uuid.UUID) (Report, error) {
	row, err := s.queries.GetReportByID(ctx, reportID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, ErrReportNotFound
	}
	if err != nil {
		return Report{}, fmt.Errorf("查询举报失败: %w", err)
	}
	return reportFromRow(row), nil
}

// requireAdmin 在服务层再查一次角色，而不是信任路由或前端。
// 举报队列会暴露举报人与被举报人的信息，越权读取比普通内容泄露更敏感。
func (s *Service) requireAdmin(ctx context.Context, userID uuid.UUID) error {
	admin, err := s.queries.IsAdmin(ctx, userID)
	if err != nil {
		return fmt.Errorf("查询用户角色失败: %w", err)
	}
	if !admin {
		return ErrForbidden
	}
	return nil
}

// ---------------------------------------------------------------------------
// 屏蔽
// ---------------------------------------------------------------------------

// Block 屏蔽一个用户，立即生效。
//
// 屏蔽关系只在查询层过滤信息流，不删除任何内容：
// 被屏蔽者的主页仍可访问（PRD F-06），这是「我不看」而不是「他消失」。
func (s *Service) Block(ctx context.Context, actorID uuid.UUID, handle string) error {
	targetID, err := s.resolveUser(ctx, handle)
	if err != nil {
		return err
	}
	if targetID == actorID {
		return fmt.Errorf("%w: 不能屏蔽自己", ErrValidation)
	}
	affected, err := s.queries.CreateBlock(ctx, BlockParams{BlockerID: actorID, BlockedID: targetID})
	if err != nil {
		return fmt.Errorf("屏蔽用户失败: %w", err)
	}
	if affected == 0 {
		return ErrBlocked
	}
	return nil
}

// Unblock 取消屏蔽。幂等性由领域错误表达：没屏蔽过返回 ErrNotBlocked。
func (s *Service) Unblock(ctx context.Context, actorID uuid.UUID, handle string) error {
	targetID, err := s.resolveUser(ctx, handle)
	if err != nil {
		return err
	}
	affected, err := s.queries.DeleteBlock(ctx, BlockParams{BlockerID: actorID, BlockedID: targetID})
	if err != nil {
		return fmt.Errorf("取消屏蔽失败: %w", err)
	}
	if affected == 0 {
		return ErrNotBlocked
	}
	return nil
}

// ListBlocks 列出当前用户屏蔽的人，按屏蔽时间倒序游标翻页。
func (s *Service) ListBlocks(ctx context.Context, actorID uuid.UUID, rawCursor string, limit int32) (BlocksResult, error) {
	limit = normalizeLimit(limit)
	createdAt, id, err := decodeCursor(rawCursor, "blocks")
	if err != nil {
		return BlocksResult{}, err
	}
	rows, err := s.queries.ListBlocks(ctx, ListBlocksParams{
		BlockerID: actorID,
		CursorAt:  timeCursor(createdAt, rawCursor),
		CursorID:  uuidParam(id),
		Limit:     limit + 1,
	})
	if err != nil {
		return BlocksResult{}, fmt.Errorf("查询屏蔽列表失败: %w", err)
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	blocked := make([]BlockedUser, 0, len(rows))
	for _, row := range rows {
		blocked = append(blocked, BlockedUser{
			ID: row.UserID, Name: row.Name, Handle: row.Handle,
			VerifyType: row.VerifyType, BlockedAt: row.CreatedAt.UTC(),
		})
	}
	var next *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := encodeCursor("blocks", last.CreatedAt, last.UserID)
		if err != nil {
			return BlocksResult{}, err
		}
		next = &encoded
	}
	return BlocksResult{Blocked: blocked, NextCursor: next}, nil
}

// Blocked 判断当前用户是否屏蔽了 handle 对应的用户。
// 供个人主页按钮初始化状态使用；handle 不存在时返回 false。
func (s *Service) Blocked(ctx context.Context, actorID uuid.UUID, handle string) (bool, error) {
	targetID, err := s.queries.GetUserIDByHandle(ctx, strings.TrimSpace(handle))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrUserNotFound
	}
	if err != nil {
		return false, fmt.Errorf("查询用户失败: %w", err)
	}
	blocked, err := s.queries.IsBlocked(ctx, BlockParams{BlockerID: actorID, BlockedID: targetID})
	if err != nil {
		return false, fmt.Errorf("查询屏蔽状态失败: %w", err)
	}
	return blocked, nil
}

// resolveUser 把句柄解析为用户 ID。句柄不存在与已停用统一返回 ErrUserNotFound。
func (s *Service) resolveUser(ctx context.Context, handle string) (uuid.UUID, error) {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return uuid.Nil, fmt.Errorf("%w: 句柄不能为空", ErrValidation)
	}
	id, err := s.queries.GetUserIDByHandle(ctx, handle)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrUserNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("查询用户失败: %w", err)
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// 内部辅助
// ---------------------------------------------------------------------------

// validReportStatuses 把「可筛选的状态」与「可设置的状态」分开：
// 列表筛选允许 pending（运营要看新提交的），修改状态不允许。
func validReportStatuses() map[string]struct{} {
	return map[string]struct{}{
		"pending": {}, "reviewing": {}, "resolved": {}, "rejected": {},
	}
}

func reportFromRow(row ReportRow) Report {
	report := Report{
		ID:         row.ID,
		TargetType: row.TargetType,
		TargetID:   row.TargetID,
		Reason:     row.Reason,
		Detail:     stringValue(row.Detail),
		Status:     row.Status,
		Reporter: &UserRef{
			ID: row.ReporterID, Name: row.ReporterName, Handle: row.ReporterHandle,
		},
		CreatedAt: row.CreatedAt.UTC(),
	}
	if row.HandlerID.Valid {
		report.HandledBy = &UserRef{
			ID: row.HandlerID.Bytes, Name: stringValue(row.HandlerName), Handle: stringValue(row.HandlerHandle),
		}
	}
	if row.HandledAt.Valid {
		handledAt := row.HandledAt.Time.UTC()
		report.HandledAt = &handledAt
	}
	return report
}

func normalizeLimit(limit int32) int32 {
	if limit <= 0 {
		return 20
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func timeCursor(value time.Time, raw string) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: raw != ""}
}

func uuidParam(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
