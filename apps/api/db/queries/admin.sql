-- 运营后台查询（数据概览 / 用户管理 / 内容管理 / 文件排查）。
--
-- 约定：
--   * 所有查询只读，写操作（封禁、删除）在 admin.sql 的写段；
--   * 角色判定不在 SQL 层做，统一由服务层 requireAdmin 完成——
--     与 moderation.sql 的举报队列保持一致；
--   * 统计口径统一排除软删除行（deleted_at IS NULL），否则「用户总数」
--     会把注销账号也算进去，运营看到的数字就不可信了；
--   * 时间窗口一律以服务端 now() 为基准，避免客户端时区差异。

-- ---------------------------------------------------------------------------
-- 概览统计
-- ---------------------------------------------------------------------------

-- name: AdminOverviewTotals :one
-- 一次性取回所有实体总量。单条语句而不是七个 count(*)：概览页每次刷新
-- 都会调用，多次往返只会把首屏拖慢，而这条查询走的是各自表的索引/统计。
SELECT
    (SELECT count(*) FROM users  WHERE deleted_at IS NULL)               AS users,
    (SELECT count(*) FROM users  WHERE deleted_at IS NULL AND status = 'active') AS active_users,
    (SELECT count(*) FROM users  WHERE deleted_at IS NULL AND role = 'admin')    AS admins,
    (SELECT count(*) FROM posts  WHERE deleted_at IS NULL)               AS posts,
    (SELECT count(*) FROM posts  WHERE deleted_at IS NULL AND visibility = 'public') AS public_posts,
    (SELECT count(*) FROM comments WHERE deleted_at IS NULL)             AS comments,
    (SELECT count(*) FROM papers)                                        AS papers,
    (SELECT count(*) FROM papers WHERE pdf_file_id IS NOT NULL)          AS papers_with_pdf,
    (SELECT count(*) FROM groups WHERE deleted_at IS NULL)               AS groups,
    (SELECT count(*) FROM files)                                         AS files,
    (SELECT count(*) FROM files WHERE status = 'failed')                 AS failed_files,
    -- 显式 cast：sqlc 无法从 coalesce(sum(bigint)) 推断类型，会生成 interface{}。
    (SELECT coalesce(sum(size_bytes), 0)::bigint FROM files)             AS storage_bytes,
    (SELECT count(*) FROM documents WHERE deleted_at IS NULL)            AS documents,
    (SELECT count(*) FROM reports WHERE status = 'pending')              AS pending_reports,
    (SELECT count(*) FROM blocks)                                        AS blocks;

-- name: AdminDailyActivity :many
-- 近 N 天的新增趋势：用户、帖子、论文、评论按天聚合。
--
-- 用 generate_series 生成完整日期轴，再 LEFT JOIN 各表——直接对表 group by
-- 会漏掉没有数据的日期，前端画折线图时会出现断点。
-- 各表的 created_at 上都有索引或至少按时间有序，窗口限定在 30 天内。
WITH days AS (
    SELECT generate_series(
        date_trunc('day', now()) - (sqlc.arg('days')::int - 1) * interval '1 day',
        date_trunc('day', now()),
        interval '1 day'
    )::date AS day
)
SELECT
    d.day,
    (SELECT count(*) FROM users u
      WHERE u.deleted_at IS NULL AND u.created_at >= d.day AND u.created_at < d.day + interval '1 day') AS new_users,
    (SELECT count(*) FROM posts p
      WHERE p.deleted_at IS NULL AND p.created_at >= d.day AND p.created_at < d.day + interval '1 day') AS new_posts,
    (SELECT count(*) FROM papers pa
      WHERE pa.created_at >= d.day AND pa.created_at < d.day + interval '1 day') AS new_papers,
    (SELECT count(*) FROM comments c
      WHERE c.deleted_at IS NULL AND c.created_at >= d.day AND c.created_at < d.day + interval '1 day') AS new_comments
FROM days d
ORDER BY d.day;

-- name: AdminTopTags :many
-- 话题热度榜：按公开帖子数排序，给运营看社区关注点。
SELECT t.slug, t.name, count(pt.post_id)::bigint AS post_count
FROM tags t
JOIN post_tags pt ON pt.tag_id = t.id
JOIN posts p ON p.id = pt.post_id
WHERE p.deleted_at IS NULL AND p.visibility = 'public'
GROUP BY t.id, t.slug, t.name
ORDER BY post_count DESC, t.name
LIMIT sqlc.arg('limit_count')::int;

-- ---------------------------------------------------------------------------
-- 用户管理
-- ---------------------------------------------------------------------------

-- name: AdminListUsers :many
-- 用户列表：支持按句柄/姓名/邮箱模糊搜索，按注册时间倒序游标分页。
--
-- 搜索用 ILIKE 而不是全文索引：后台是运营小流量场景，数据量在十万级以内，
-- 配合 trigram 索引（若已建）足够；引入 tsvector 只会增加维护成本。
SELECT u.id, u.email, u.name, u.handle, u.role, u.status,
       u.verify_type, u.institution, u.created_at, u.last_active_at,
       (SELECT count(*) FROM posts p WHERE p.author_id = u.id AND p.deleted_at IS NULL) AS post_count,
       (SELECT count(*) FROM follows f WHERE f.followee_id = u.id) AS follower_count
FROM users u
WHERE u.deleted_at IS NULL
  AND (sqlc.narg('query')::text IS NULL
       OR u.handle ILIKE sqlc.narg('query')::text
       OR u.name   ILIKE sqlc.narg('query')::text
       OR u.email::text ILIKE sqlc.narg('query')::text)
  AND (sqlc.narg('role')::user_role IS NULL OR u.role = sqlc.narg('role')::user_role)
  AND (sqlc.narg('status')::text IS NULL OR u.status = sqlc.narg('status')::text)
  AND (sqlc.narg('cursor_at')::timestamptz IS NULL
       OR (u.created_at, u.id) < (sqlc.narg('cursor_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY u.created_at DESC, u.id DESC
LIMIT sqlc.arg('limit_count')::int;

-- name: AdminCountUsers :one
SELECT count(*) FROM users WHERE deleted_at IS NULL;

-- name: AdminSetUserRole :one
UPDATE users SET role = sqlc.arg('role')::user_role, updated_at = now()
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING id, handle, role, status;

-- name: AdminSetUserStatus :one
-- 封禁 / 解封。status 是 varchar 而非枚举，取值在服务层白名单校验。
-- 封禁后 GetByID 会拒绝该用户（见 identity.GetByID），会话立即失效。
UPDATE users SET status = sqlc.arg('status'), updated_at = now()
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING id, handle, role, status;

-- name: AdminCountAdmins :one
-- 防止把最后一个管理员降级或封禁，导致后台彻底失去入口。
SELECT count(*) FROM users
WHERE role = 'admin' AND status = 'active' AND deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- 内容管理
-- ---------------------------------------------------------------------------

-- name: AdminListPosts :many
SELECT p.id, p.author_id, u.handle AS author_handle, u.name AS author_name,
       p.type, p.visibility, p.group_id,
       left(coalesce(p.content_text, ''), 200)::text AS excerpt,
       p.paper_id, p.like_count, p.comment_count, p.repost_count,
       p.created_at, p.deleted_at
FROM posts p
JOIN users u ON u.id = p.author_id
WHERE (sqlc.narg('include_deleted')::boolean IS NOT TRUE AND p.deleted_at IS NULL)
  AND (sqlc.narg('query')::text IS NULL OR p.content_text ILIKE sqlc.narg('query')::text)
  AND (sqlc.narg('cursor_at')::timestamptz IS NULL
       OR (p.created_at, p.id) < (sqlc.narg('cursor_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY p.created_at DESC, p.id DESC
LIMIT sqlc.arg('limit_count')::int;

-- name: AdminSoftDeletePost :one
UPDATE posts SET deleted_at = now(), updated_at = now()
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING id, deleted_at;

-- name: AdminRestorePost :one
-- 恢复被误删的帖子。管理后台的「删除」是软删除，必须可逆。
UPDATE posts SET deleted_at = NULL, updated_at = now()
WHERE id = sqlc.arg('id') AND deleted_at IS NOT NULL
RETURNING id, deleted_at;

-- name: AdminListPapers :many
SELECT p.id, p.title, p.venue, p.published_at, p.citation_count,
       p.meta_source, p.pdf_file_id, p.created_at,
       jsonb_array_length(p.authors) AS author_count,
       (SELECT count(*) FROM posts po WHERE po.paper_id = p.id AND po.deleted_at IS NULL) AS post_count
FROM papers p
WHERE (sqlc.narg('query')::text IS NULL
       OR p.title ILIKE sqlc.narg('query')::text
       OR p.title_norm ILIKE sqlc.narg('query')::text)
  AND (sqlc.narg('cursor_at')::timestamptz IS NULL
       OR (p.created_at, p.id) < (sqlc.narg('cursor_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY p.created_at DESC, p.id DESC
LIMIT sqlc.arg('limit_count')::int;

-- name: AdminListGroups :many
SELECT g.id, g.slug, g.name, g.description, g.owner_id,
       u.handle AS owner_handle, u.name AS owner_name,
       g.join_policy, g.member_count, g.created_at, g.deleted_at,
       (SELECT count(*) FROM documents d WHERE d.group_id = g.id AND d.deleted_at IS NULL) AS document_count
FROM groups g
JOIN users u ON u.id = g.owner_id
WHERE (sqlc.narg('include_deleted')::boolean IS NOT TRUE AND g.deleted_at IS NULL)
  AND (sqlc.narg('query')::text IS NULL
       OR g.name ILIKE sqlc.narg('query')::text
       OR g.slug ILIKE sqlc.narg('query')::text)
  AND (sqlc.narg('cursor_at')::timestamptz IS NULL
       OR (g.created_at, g.id) < (sqlc.narg('cursor_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY g.created_at DESC, g.id DESC
LIMIT sqlc.arg('limit_count')::int;

-- name: AdminSoftDeleteGroup :one
UPDATE groups SET deleted_at = now()
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING id, deleted_at;

-- name: AdminRestoreGroup :one
UPDATE groups SET deleted_at = NULL
WHERE id = sqlc.arg('id') AND deleted_at IS NOT NULL
RETURNING id, deleted_at;

-- ---------------------------------------------------------------------------
-- 文件与存储
-- ---------------------------------------------------------------------------

-- name: AdminListFiles :many
-- 文件排查列表：默认只看失败与处理中的，这些才是用户会来投诉的。
SELECT f.id, f.owner_id, u.handle AS owner_handle, u.name AS owner_name,
       f.original_name, f.kind, f.mime, f.size_bytes, f.status,
       f.page_count, f.error_reason, f.retry_count, f.created_at, f.ready_at
FROM files f
JOIN users u ON u.id = f.owner_id
WHERE (sqlc.narg('status')::file_status IS NULL OR f.status = sqlc.narg('status')::file_status)
  AND (sqlc.narg('only_problem')::boolean IS NOT TRUE
       OR f.status IN ('failed', 'rejected', 'transcoding', 'pending'))
  AND (sqlc.narg('cursor_at')::timestamptz IS NULL
       OR (f.created_at, f.id) < (sqlc.narg('cursor_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY f.created_at DESC, f.id DESC
LIMIT sqlc.arg('limit_count')::int;

-- name: AdminFileStats :many
-- 按状态汇总文件数与占用空间，用于判断失败率与存储分布。
SELECT status, count(*)::bigint AS file_count, coalesce(sum(size_bytes), 0)::bigint AS bytes
FROM files
GROUP BY status
ORDER BY file_count DESC;

-- name: AdminIsAdmin :one
-- 与 moderation.sql 的 IsAdmin 同义。单独放一份是为了让 admin 包
-- 只依赖自己的查询集，不必为了一个角色判定去穿透 db.Queries 的私有字段。
SELECT role = 'admin' FROM users
WHERE id = sqlc.arg('user_id') AND deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- 评论管理
-- ---------------------------------------------------------------------------

-- name: AdminListComments :many
-- 评论管理列表：按创建时间倒序，支持正文模糊搜索与「含已删除」。
-- 与帖子列表保持同一套游标语义（created_at, id）。
SELECT c.id, c.post_id, c.parent_id, c.author_id,
       u.handle AS author_handle, u.name AS author_name,
       left(c.content_text, 200)::text AS excerpt,
       c.like_count, c.created_at, c.deleted_at,
       p.visibility AS post_visibility, p.deleted_at AS post_deleted_at
FROM comments c
JOIN users u ON u.id = c.author_id
JOIN posts p ON p.id = c.post_id
WHERE (sqlc.narg('include_deleted')::boolean IS NOT TRUE AND c.deleted_at IS NULL)
  AND (sqlc.narg('query')::text IS NULL OR c.content_text ILIKE sqlc.narg('query')::text)
  AND (sqlc.narg('cursor_at')::timestamptz IS NULL
       OR (c.created_at, c.id) < (sqlc.narg('cursor_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY c.created_at DESC, c.id DESC
LIMIT sqlc.arg('limit_count')::int;

-- name: AdminSoftDeleteComment :one
-- 软删除评论并同步减少帖子的 comment_count。
--
-- 为什么在这里改计数而不是依赖触发器：项目其它计数（like_count 等）
-- 都在应用层维护，保持同一套约定，避免出现两套口径。
-- GREATEST(..., 0) 兜底：历史数据里若有计数漂移，减法不能让计数变负。
WITH target AS (
    UPDATE comments SET deleted_at = now()
    WHERE comments.id = sqlc.arg('id') AND comments.deleted_at IS NULL
    RETURNING comments.post_id
)
UPDATE posts SET comment_count = GREATEST(posts.comment_count - 1, 0), updated_at = now()
WHERE posts.id = (SELECT post_id FROM target)
RETURNING posts.id;

-- name: AdminRestoreComment :one
-- 恢复评论并补回计数。
WITH target AS (
    UPDATE comments SET deleted_at = NULL
    WHERE comments.id = sqlc.arg('id') AND comments.deleted_at IS NOT NULL
    RETURNING comments.post_id
)
UPDATE posts SET comment_count = posts.comment_count + 1, updated_at = now()
WHERE posts.id = (SELECT post_id FROM target)
RETURNING posts.id;
