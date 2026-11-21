-- 举报与屏蔽查询。
--
-- 约定：
--   * 举报队列只给运营（role='admin'）看，角色判定在服务层用 IsAdmin 完成；
--   * 屏蔽关系只在查询层过滤内容（见 posts.sql 的四个 feed 查询），
--     本文件不负责过滤，只负责增删查；
--   * 「同一用户对同一目标只举报一次」由 LockReporter + ReportExists + CreateReport
--     在同一事务里保证；表上没有对应的唯一索引（历史 schema 未加，避免迁移改表）。

-- name: LockReporter :one
-- 「同一用户对同一目标只能举报一次」在 reports 上没有唯一索引（历史 schema），
-- 因此在事务里先锁住举报人这一行，让同一举报人的并发提交串行化，
-- 再查重、再插入。粒度是单个用户，不同用户之间不互相阻塞。
SELECT id FROM users WHERE id = sqlc.arg('reporter_id') FOR UPDATE;

-- name: ReportExists :one
SELECT EXISTS(
    SELECT 1 FROM reports
    WHERE reporter_id = sqlc.arg('reporter_id')
      AND target_type = sqlc.arg('target_type')
      AND target_id = sqlc.arg('target_id')
);

-- name: CreateReport :one
INSERT INTO reports (reporter_id, target_type, target_id, reason, detail)
VALUES (sqlc.arg('reporter_id'), sqlc.arg('target_type'), sqlc.arg('target_id'),
        sqlc.arg('reason'), sqlc.narg('detail'))
RETURNING id;

-- name: GetReportByID :one
SELECT r.id, r.target_type, r.target_id, r.reason, r.detail, r.status,
       r.reporter_id, reporter.name AS reporter_name, reporter.handle AS reporter_handle,
       r.handled_by, handler.name AS handler_name, handler.handle AS handler_handle,
       r.handled_at, r.created_at
FROM reports r
JOIN users reporter ON reporter.id = r.reporter_id
LEFT JOIN users handler ON handler.id = r.handled_by
WHERE r.id = sqlc.arg('id');

-- name: ListReports :many
-- status 为 NULL 表示不筛选；游标为空时 cursor_at / cursor_id 传 NULL，条件整体放行。
SELECT r.id, r.target_type, r.target_id, r.reason, r.detail, r.status,
       r.reporter_id, reporter.name AS reporter_name, reporter.handle AS reporter_handle,
       r.handled_by, handler.name AS handler_name, handler.handle AS handler_handle,
       r.handled_at, r.created_at
FROM reports r
JOIN users reporter ON reporter.id = r.reporter_id
LEFT JOIN users handler ON handler.id = r.handled_by
WHERE (sqlc.narg('status')::report_status IS NULL OR r.status = sqlc.narg('status')::report_status)
  AND (sqlc.narg('cursor_at')::timestamptz IS NULL
       OR (r.created_at, r.id) < (sqlc.narg('cursor_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY r.created_at DESC, r.id DESC
LIMIT sqlc.arg('limit');

-- name: UpdateReportStatus :execrows
UPDATE reports
SET status = sqlc.arg('status')::report_status,
    handled_by = sqlc.arg('handled_by'),
    handled_at = now()
WHERE id = sqlc.arg('id');

-- name: IsAdmin :one
SELECT role = 'admin' FROM users WHERE id = sqlc.arg('user_id') AND deleted_at IS NULL;

-- name: GetUserIDByHandle :one
SELECT id FROM users
WHERE handle = sqlc.arg('handle') AND deleted_at IS NULL AND status = 'active';

-- name: GetTargetAuthor :one
-- 按目标类型解析作者。四类目标 UNION ALL 成单条语句，服务层只需区分
-- 「有作者」与「目标不存在」。已软删除的目标视为不存在。
SELECT author FROM (
    SELECT p.author_id AS author FROM posts p
    WHERE sqlc.arg('target_type')::text = 'post' AND p.id = sqlc.arg('target_id')::uuid AND p.deleted_at IS NULL
    UNION ALL
    SELECT c.author_id FROM comments c
    WHERE sqlc.arg('target_type')::text = 'comment' AND c.id = sqlc.arg('target_id')::uuid AND c.deleted_at IS NULL
    UNION ALL
    SELECT u.id FROM users u
    WHERE sqlc.arg('target_type')::text = 'user' AND u.id = sqlc.arg('target_id')::uuid AND u.deleted_at IS NULL
    UNION ALL
    SELECT d.created_by FROM documents d
    WHERE sqlc.arg('target_type')::text = 'document' AND d.id = sqlc.arg('target_id')::uuid AND d.deleted_at IS NULL
) targets
LIMIT 1;

-- name: CreateBlock :execrows
-- 幂等：重复屏蔽返回 0 行，服务层映射为 ErrBlocked。
INSERT INTO blocks (blocker_id, blocked_id) VALUES (sqlc.arg('blocker_id'), sqlc.arg('blocked_id'))
ON CONFLICT (blocker_id, blocked_id) DO NOTHING;

-- name: DeleteBlock :execrows
DELETE FROM blocks WHERE blocker_id = sqlc.arg('blocker_id') AND blocked_id = sqlc.arg('blocked_id');

-- name: ListBlocks :many
SELECT b.blocked_id AS user_id, u.name, u.handle, u.verify_type, b.created_at
FROM blocks b
JOIN users u ON u.id = b.blocked_id
WHERE b.blocker_id = sqlc.arg('blocker_id')
  AND (sqlc.narg('cursor_at')::timestamptz IS NULL
       OR (b.created_at, b.blocked_id) < (sqlc.narg('cursor_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY b.created_at DESC, b.blocked_id DESC
LIMIT sqlc.arg('limit');

-- name: IsBlocked :one
SELECT EXISTS(SELECT 1 FROM blocks WHERE blocker_id = sqlc.arg('blocker_id') AND blocked_id = sqlc.arg('blocked_id'));
