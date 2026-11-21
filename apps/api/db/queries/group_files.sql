-- 群文件库（PRD G-05）。
--
-- 约定：
--   * 群文件库保存的是「指向已有文件的引用」（group_files.file_id），
--     不是文件副本；同一文件在同一群里只登记一次（唯一约束）。
--   * 从帖子存入时记录 post_id，便于回到原始讨论上下文。
--   * 标签用 text[] + GIN 索引，按标签过滤用包含查询即可。

-- name: GetGroupFileContext :one
-- 群组解析与当前用户角色一次取回；my_role 为空串表示非群成员。
--
-- 用 COALESCE 而不是让 NULL 直出：非成员是最常见的调用场景（探测权限），
-- 让它在 sqlc 里映射成 pgtype.Text / *string 会迫使每个调用点都判空；
-- 领域层本来就用空串表示「无角色」，这里直接对齐。
SELECT g.id AS group_id, g.owner_id, COALESCE(gm.role::text, '') AS my_role
FROM groups g
LEFT JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = sqlc.arg('user_id')
WHERE g.slug = sqlc.arg('slug') AND g.deleted_at IS NULL;

-- name: GetFileForGroupFile :one
-- 校验文件存在，并找出「本群内引用该文件的最新帖子」作为来源。
-- 来源帖子既是审计信息，也是权限依据：文件属于上传者本人，或已被本群帖子引用。
SELECT f.id, f.owner_id, f.original_name, f.mime, f.kind::text AS kind,
       f.size_bytes, COALESCE(f.page_count, 0)::integer AS page_count,
       (SELECT pa.post_id
          FROM post_attachments pa
          JOIN posts p ON p.id = pa.post_id
         WHERE pa.file_id = f.id
           AND p.group_id = sqlc.arg('group_id')
           AND p.deleted_at IS NULL
         ORDER BY p.created_at DESC
         LIMIT 1) AS post_id
FROM files f
WHERE f.id = sqlc.arg('file_id');

-- name: InsertGroupFile :one
-- ON CONFLICT DO NOTHING：重复存入不是错误，而是「已存在」这一业务结果，
-- 由调用方转成 409。返回 0 行即表示冲突。
INSERT INTO group_files (group_id, file_id, post_id, title, description, tags, added_by)
VALUES (
    sqlc.arg('group_id'), sqlc.arg('file_id'), sqlc.narg('post_id'),
    sqlc.arg('title'), sqlc.narg('description'), sqlc.arg('tags'), sqlc.arg('added_by')
)
ON CONFLICT (group_id, file_id) DO NOTHING
RETURNING id;

-- name: GetGroupFile :one
SELECT gf.id, gf.group_id, gf.file_id, gf.post_id, gf.title,
       COALESCE(gf.description, '') AS description, gf.tags,
       gf.added_by, u.name AS added_by_name, u.handle AS added_by_handle,
       gf.created_at,
       f.original_name, f.mime, f.kind::text AS kind, f.size_bytes,
       COALESCE(f.page_count, 0)::integer AS page_count
FROM group_files gf
JOIN users u ON u.id = gf.added_by
JOIN files f ON f.id = gf.file_id
WHERE gf.group_id = sqlc.arg('group_id') AND gf.id = sqlc.arg('id');

-- name: ListGroupFiles :many
-- 按 (created_at, id) 倒序翻页；tag 为空时不参与过滤。
SELECT gf.id, gf.group_id, gf.file_id, gf.post_id, gf.title,
       COALESCE(gf.description, '') AS description, gf.tags,
       gf.added_by, u.name AS added_by_name, u.handle AS added_by_handle,
       gf.created_at,
       f.original_name, f.mime, f.kind::text AS kind, f.size_bytes,
       COALESCE(f.page_count, 0)::integer AS page_count
FROM group_files gf
JOIN users u ON u.id = gf.added_by
JOIN files f ON f.id = gf.file_id
WHERE gf.group_id = sqlc.arg('group_id')
  AND (sqlc.narg('tag')::text IS NULL OR sqlc.narg('tag') = ANY(gf.tags))
  AND (sqlc.narg('before_created_at')::timestamptz IS NULL
       OR (gf.created_at, gf.id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.arg('before_id')::uuid))
ORDER BY gf.created_at DESC, gf.id DESC
LIMIT sqlc.arg('limit');

-- name: DeleteGroupFile :execrows
-- 同样带上 group_id，避免跨群删除。
DELETE FROM group_files
WHERE group_id = sqlc.arg('group_id') AND id = sqlc.arg('id');
