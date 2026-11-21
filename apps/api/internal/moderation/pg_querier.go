package moderation

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// dbtx 是 pgx 连接池与事务的公共子集。
//
// 这里直接写 SQL 而不是等 sqlc 生成：举报与屏蔽的查询尚在 db/queries/moderation.sql
// 里待生成，先由本文件承担同样的契约，服务层只依赖 Querier 接口。
// 接线代理生成后，可把 newPGQuerier 的实现替换成 db.Queries 的适配器，
// 接口与字段名保持不变。
type dbtx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type pgQuerier struct{ db dbtx }

func newPGQuerier(db dbtx) *pgQuerier { return &pgQuerier{db: db} }

// LockReporter 锁住举报人这一行，让同一举报人的并发提交串行化。
//
// reports 表没有 (reporter_id, target_type, target_id) 唯一索引，
// 「先查重、再插入」在并发下会各写一行。用行锁而不是咨询锁：
// 键就是现成的用户主键，不存在哈希碰撞，也不依赖额外的锁命名约定。
func (q *pgQuerier) LockReporter(ctx context.Context, reporterID uuid.UUID) error {
	var id pgtype.UUID
	return q.db.QueryRow(ctx, `
SELECT id FROM users WHERE id = $1 FOR UPDATE`, reporterID).Scan(&id)
}

func (q *pgQuerier) ReportExists(ctx context.Context, arg CreateReportParams) (bool, error) {
	var exists bool
	err := q.db.QueryRow(ctx, `
SELECT EXISTS(
    SELECT 1 FROM reports
    WHERE reporter_id = $1 AND target_type = $2 AND target_id = $3
)`, arg.ReporterID, arg.TargetType, arg.TargetID).Scan(&exists)
	return exists, err
}

func (q *pgQuerier) CreateReport(ctx context.Context, arg CreateReportParams) (uuid.UUID, error) {
	var id pgtype.UUID
	err := q.db.QueryRow(ctx, `
INSERT INTO reports (reporter_id, target_type, target_id, reason, detail)
VALUES ($1, $2, $3, $4, $5)
RETURNING id`,
		arg.ReporterID, arg.TargetType, arg.TargetID, arg.Reason, arg.Detail,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	return id.Bytes, nil
}

// reportSelectSQL 是举报列表与详情共用的投影。
// 处理人 LEFT JOIN：未处理时 handler 相关列全为 NULL。
const reportSelectSQL = `
SELECT r.id, r.target_type, r.target_id, r.reason, r.detail, r.status,
       r.reporter_id, reporter.name AS reporter_name, reporter.handle AS reporter_handle,
       r.handled_by, handler.name AS handler_name, handler.handle AS handler_handle,
       r.handled_at, r.created_at
FROM reports r
JOIN users reporter ON reporter.id = r.reporter_id
LEFT JOIN users handler ON handler.id = r.handled_by`

func scanReportRow(row pgx.Row) (ReportRow, error) {
	var out ReportRow
	err := row.Scan(
		&out.ID, &out.TargetType, &out.TargetID, &out.Reason, &out.Detail, &out.Status,
		&out.ReporterID, &out.ReporterName, &out.ReporterHandle,
		&out.HandlerID, &out.HandlerName, &out.HandlerHandle,
		&out.HandledAt, &out.CreatedAt,
	)
	return out, err
}

func (q *pgQuerier) GetReportByID(ctx context.Context, id uuid.UUID) (ReportRow, error) {
	return scanReportRow(q.db.QueryRow(ctx, reportSelectSQL+` WHERE r.id = $1`, id))
}

func (q *pgQuerier) ListReports(ctx context.Context, arg ListReportsParams) ([]ReportRow, error) {
	const query = reportSelectSQL + `
WHERE ($1::report_status IS NULL OR r.status = $1)
  AND ($2::timestamptz IS NULL OR (r.created_at, r.id) < ($2::timestamptz, $3::uuid))
ORDER BY r.created_at DESC, r.id DESC
LIMIT $4`
	rows, err := q.db.Query(ctx, query, arg.Status, arg.CursorAt, arg.CursorID, arg.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ReportRow, 0, arg.Limit)
	for rows.Next() {
		row, err := scanReportRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (q *pgQuerier) UpdateReportStatus(ctx context.Context, arg UpdateReportStatusParams) (int64, error) {
	const query = `
UPDATE reports
SET status = $2, handled_by = $3, handled_at = now()
WHERE id = $1`
	tag, err := q.db.Exec(ctx, query, arg.ID, arg.Status, arg.HandledBy)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *pgQuerier) IsAdmin(ctx context.Context, userID uuid.UUID) (bool, error) {
	var admin bool
	err := q.db.QueryRow(ctx, `
SELECT role = 'admin' FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&admin)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return admin, err
}

func (q *pgQuerier) GetUserIDByHandle(ctx context.Context, handle string) (uuid.UUID, error) {
	var id pgtype.UUID
	err := q.db.QueryRow(ctx, `
SELECT id FROM users
WHERE handle = $1 AND deleted_at IS NULL AND status = 'active'`, handle).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	return id.Bytes, nil
}

// targetAuthorSQL 按目标类型解析作者。
// 四类目标各自一条分支，用 UNION ALL 合并成单条语句：
// 服务层只需区分「有作者」与「目标不存在」，不必为每种类型写一次查询。
const targetAuthorSQL = `
SELECT author FROM (
    SELECT p.author_id AS author FROM posts p
    WHERE $1::text = 'post' AND p.id = $2::uuid AND p.deleted_at IS NULL
    UNION ALL
    SELECT c.author_id FROM comments c
    WHERE $1::text = 'comment' AND c.id = $2::uuid AND c.deleted_at IS NULL
    UNION ALL
    SELECT u.id FROM users u
    WHERE $1::text = 'user' AND u.id = $2::uuid AND u.deleted_at IS NULL
    UNION ALL
    SELECT d.created_by FROM documents d
    WHERE $1::text = 'document' AND d.id = $2::uuid AND d.deleted_at IS NULL
) targets
LIMIT 1`

func (q *pgQuerier) GetTargetAuthor(ctx context.Context, arg TargetAuthorParams) (uuid.UUID, error) {
	var id pgtype.UUID
	err := q.db.QueryRow(ctx, targetAuthorSQL, arg.TargetType, arg.TargetID).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	return id.Bytes, nil
}

func (q *pgQuerier) CreateBlock(ctx context.Context, arg BlockParams) (int64, error) {
	tag, err := q.db.Exec(ctx, `
INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)
ON CONFLICT (blocker_id, blocked_id) DO NOTHING`, arg.BlockerID, arg.BlockedID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *pgQuerier) DeleteBlock(ctx context.Context, arg BlockParams) (int64, error) {
	tag, err := q.db.Exec(ctx, `
DELETE FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`, arg.BlockerID, arg.BlockedID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *pgQuerier) ListBlocks(ctx context.Context, arg ListBlocksParams) ([]BlockedUserRow, error) {
	const query = `
SELECT b.blocked_id, u.name, u.handle, u.verify_type, b.created_at
FROM blocks b
JOIN users u ON u.id = b.blocked_id
WHERE b.blocker_id = $1
  AND ($2::timestamptz IS NULL OR (b.created_at, b.blocked_id) < ($2::timestamptz, $3::uuid))
ORDER BY b.created_at DESC, b.blocked_id DESC
LIMIT $4`
	rows, err := q.db.Query(ctx, query, arg.BlockerID, arg.CursorAt, arg.CursorID, arg.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]BlockedUserRow, 0, arg.Limit)
	for rows.Next() {
		var row BlockedUserRow
		var id pgtype.UUID
		if err := rows.Scan(&id, &row.Name, &row.Handle, &row.VerifyType, &row.CreatedAt); err != nil {
			return nil, err
		}
		row.UserID = id.Bytes
		out = append(out, row)
	}
	return out, rows.Err()
}

func (q *pgQuerier) IsBlocked(ctx context.Context, arg BlockParams) (bool, error) {
	var blocked bool
	err := q.db.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM blocks WHERE blocker_id = $1 AND blocked_id = $2)`,
		arg.BlockerID, arg.BlockedID).Scan(&blocked)
	return blocked, err
}
