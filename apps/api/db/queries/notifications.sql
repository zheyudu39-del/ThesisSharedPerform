-- 通知相关查询。
--
-- 约定：
--   * 通知按类型静音：列表与未读数都排除 notification_prefs.muted_types 中的类型。
--     两者口径必须一致，否则会出现「有未读数但列表为空」的割裂。
--     未设置偏好的用户没有 notification_prefs 行，LEFT JOIN 后为 NULL，
--     由 COALESCE 视为空数组（即不静音任何类型）。
--   * 游标按 (created_at, id) 倒序翻页，ID 用于打破同一时刻的并列。
--   * payload 是 jsonb，服务层负责编码/解码。

-- name: CreateNotification :one
INSERT INTO notifications (user_id, type, actor_id, target_type, target_id, payload)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListNotifications :many
-- $1 用户，$2/$3 为 (created_at, id) 游标（首页传 NULL），$4 为 limit。
-- 同时 JOIN 出触发者摘要，省去前端逐条查询用户资料。
SELECT n.id, n.user_id, n.type, n.actor_id, n.target_type, n.target_id,
       n.payload, n.read_at, n.created_at,
       u.name AS actor_name, u.handle AS actor_handle, u.avatar_key AS actor_avatar_key
FROM notifications n
LEFT JOIN notification_prefs p ON p.user_id = n.user_id
LEFT JOIN users u ON u.id = n.actor_id
WHERE n.user_id = $1
  AND n.type <> ALL(COALESCE(p.muted_types, '{}'::notify_type[]))
  AND ($2::timestamptz IS NULL OR (n.created_at, n.id) < ($2::timestamptz, $3::uuid))
ORDER BY n.created_at DESC, n.id DESC
LIMIT $4;

-- name: CountUnread :one
-- 与 ListNotifications 使用同一静音谓词，保证角标数与列表内容一致。
SELECT COUNT(*)
FROM notifications n
LEFT JOIN notification_prefs p ON p.user_id = n.user_id
WHERE n.user_id = $1
  AND n.read_at IS NULL
  AND n.type <> ALL(COALESCE(p.muted_types, '{}'::notify_type[]));

-- name: MarkRead :execrows
-- 限本人：user_id 谓词不可省，否则可越权标记他人通知。
-- COALESCE 保证重复标记不会刷新首次已读时间，接口幂等。
UPDATE notifications SET read_at = COALESCE(read_at, now())
WHERE id = $1 AND user_id = $2;

-- name: MarkAllRead :execrows
-- 静音类型也一并标记：未读数本就不统计它们，若跳过会留下永远清不掉的隐藏未读。
UPDATE notifications SET read_at = now()
WHERE user_id = $1 AND read_at IS NULL;

-- name: GetNotificationPrefs :one
SELECT * FROM notification_prefs WHERE user_id = $1;

-- name: UpsertNotificationPrefs :one
INSERT INTO notification_prefs (user_id, muted_types, email_digest)
VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE
SET muted_types = EXCLUDED.muted_types,
    email_digest = EXCLUDED.email_digest
RETURNING *;

-- name: GetUnreadLikeAggNotification :one
-- 点赞聚合：查找同用户、同目标、未读且仍在窗口内的 like_agg 通知。
-- FOR UPDATE 让并发的聚合在事务内串行，避免两个点赞请求各自读到旧 count 后互相覆盖。
-- $4 为窗口起点（now - 10 分钟），由服务层计算，便于单元测试控制时间。
SELECT * FROM notifications
WHERE user_id = $1
  AND type = 'like_agg'
  AND target_type = $2
  AND target_id = $3
  AND read_at IS NULL
  AND created_at >= $4
ORDER BY created_at DESC
LIMIT 1
FOR UPDATE;

-- name: UpdateLikeAggNotification :one
-- 合并后刷新 payload 与 created_at：通知因此上浮到列表顶部，符合「最近有人点赞」的语义。
UPDATE notifications
SET payload = $2, actor_id = $3, created_at = now()
WHERE id = $1 AND user_id = $4
RETURNING *;

-- name: GetUnreadDocEditNotification :one
-- 文档编辑聚合：同一文档在 5 分钟窗口内的多次编辑合并为一条通知。
-- 与点赞聚合的区别是窗口更短——文档连续保存很常见，10 分钟会把整场协作压成一条，
-- 反而让人以为只改了一次。FOR UPDATE 同样用于串行化并发的合并。
-- $4 为窗口起点（now - 5 分钟），由服务层计算，便于单元测试控制时间。
SELECT * FROM notifications
WHERE user_id = $1
  AND type = 'doc_edit'
  AND target_type = $2
  AND target_id = $3
  AND read_at IS NULL
  AND created_at >= $4
ORDER BY created_at DESC
LIMIT 1
FOR UPDATE;

-- name: UpdateDocEditNotification :one
-- 合并后刷新 payload 与 created_at，让「刚有人编辑」的通知上浮到列表顶部。
UPDATE notifications
SET payload = $2, actor_id = $3, created_at = now()
WHERE id = $1 AND user_id = $4
RETURNING *;

-- ---------------------------------------------------------------------------
-- 通知收件人解析
-- ---------------------------------------------------------------------------
-- 下面几条查询不属于通知表本身，但只服务于通知的收件人计算，
-- 放在这里便于一处看清「谁会收到什么通知」。

-- name: ListUsersByHandles :many
-- 把正文里提取出的 @handle 一次性解析成用户 ID，避免逐条查询造成 N+1。
-- 只返回 active 且未删除的用户：给注销/封禁账号发通知没有意义。
-- handle 在 users 上是 varchar，这里显式转 text 以便与 text[] 比较。
SELECT id, handle FROM users
WHERE handle::text = ANY(sqlc.arg('handles')::text[])
  AND deleted_at IS NULL
  AND status = 'active';

-- name: ListGroupManagerIDs :many
-- 入群申请的收件人：群主与所有管理员。
-- 群主在 group_members 里也有一行 role='owner'，因此无需单独 UNION groups.owner_id。
SELECT user_id FROM group_members
WHERE group_id = sqlc.arg('group_id') AND role IN ('owner', 'admin');

-- name: ListDocumentEditorIDs :many
-- 文档编辑通知的收件人：文档创建者 + 拥有编辑权限的文档级成员。
-- 创建者不一定在 doc_members 里（默认权限来自群角色），所以用 UNION 去重合并。
SELECT created_by AS user_id FROM documents
WHERE id = sqlc.arg('doc_id') AND deleted_at IS NULL
UNION
SELECT user_id FROM doc_members
WHERE doc_id = sqlc.arg('doc_id') AND role IN ('owner', 'editor');
