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
