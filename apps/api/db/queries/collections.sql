-- 策展集查询。
--
-- 设计说明见 db/migrations/00002_curation.sql：精选以「集合」为单位，
-- 而不是给单条帖子打精选标记。

-- name: CreateCollection :one
-- starts_at 用 COALESCE 兜底：显式传 NULL 会覆盖列的 DEFAULT now()，
-- 而该列是 NOT NULL，会让「不指定上架时间」这种最常见的情况直接失败。
INSERT INTO collections (slug, title, description, cover_key, curator_id, visibility, starts_at, ends_at)
VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7::timestamptz, now()), $8)
RETURNING *;

-- name: CollectionSlugExists :one
SELECT EXISTS(SELECT 1 FROM collections WHERE slug = $1 AND deleted_at IS NULL);

-- name: GetCollectionBySlug :one
-- 带上策展人摘要，避免详情页再查一次用户。
SELECT c.*, u.name AS curator_name, u.handle AS curator_handle, u.verify_type AS curator_verify_type
FROM collections c
JOIN users u ON u.id = c.curator_id
WHERE c.slug = $1 AND c.deleted_at IS NULL
LIMIT 1;

-- name: GetCollectionByID :one
SELECT c.*, u.name AS curator_name, u.handle AS curator_handle, u.verify_type AS curator_verify_type
FROM collections c
JOIN users u ON u.id = c.curator_id
WHERE c.id = $1 AND c.deleted_at IS NULL
LIMIT 1;

-- name: UpdateCollection :one
-- 局部更新：参数为 NULL 表示「本次不修改」。
-- 注意 ends_at 无法通过本语句清空（NULL 被解释为不修改）；
-- 需要提前下架时传一个过去的时间点即可。
UPDATE collections SET
    title       = COALESCE(sqlc.narg('title')::varchar(128), title),
    description = COALESCE(sqlc.narg('description')::varchar(500), description),
    starts_at   = COALESCE(sqlc.narg('starts_at')::timestamptz, starts_at),
    ends_at     = COALESCE(sqlc.narg('ends_at')::timestamptz, ends_at),
    updated_at  = now()
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteCollection :exec
UPDATE collections SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL;

-- name: ListActiveCollections :many
-- 列表口径：未删除、公开、已开始、未结束。
-- 排序键 (starts_at DESC, id DESC)：同一时刻创建的集合用 ID 打破并列，
-- 保证游标翻页稳定。游标为 NULL 时从第一页开始。
SELECT c.*, u.name AS curator_name, u.handle AS curator_handle, u.verify_type AS curator_verify_type
FROM collections c
JOIN users u ON u.id = c.curator_id
WHERE c.deleted_at IS NULL
  AND c.visibility = 'public'
  AND c.starts_at <= now()
  AND (c.ends_at IS NULL OR c.ends_at > now())
  AND (
    sqlc.narg('cursor_time')::timestamptz IS NULL
    OR (c.starts_at, c.id) < (sqlc.narg('cursor_time')::timestamptz, sqlc.narg('cursor_id')::uuid)
  )
ORDER BY c.starts_at DESC, c.id DESC
LIMIT sqlc.arg('limit');

-- name: AddCollectionItem :one
INSERT INTO collection_items (collection_id, post_id, paper_id, note, position, added_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListCollectionItems :many
-- 按编辑排序；position 相同（多为 0）时按加入时间升序，保证稳定。
SELECT ci.*, u.name AS added_by_name, u.handle AS added_by_handle
FROM collection_items ci
JOIN users u ON u.id = ci.added_by
WHERE ci.collection_id = $1
ORDER BY ci.position ASC, ci.created_at ASC, ci.id ASC;

-- name: GetCollectionItem :one
SELECT * FROM collection_items WHERE id = $1 AND collection_id = $2;

-- name: RemoveCollectionItem :exec
DELETE FROM collection_items WHERE id = $1 AND collection_id = $2;

-- name: ReorderCollectionItem :exec
UPDATE collection_items SET position = $3 WHERE id = $1 AND collection_id = $2;

-- name: RecomputeCollectionItemCount :one
-- item_count 是冗余计数：策展集条目少、读多写少，每次变更后重算比维护增量简单。
UPDATE collections
SET item_count = (SELECT COUNT(*) FROM collection_items WHERE collection_id = $1)
WHERE id = $1
RETURNING item_count;
