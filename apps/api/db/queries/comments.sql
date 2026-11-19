-- name: CreateComment :one
INSERT INTO comments (post_id, parent_id, author_id, content_html, content_text)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetCommentParent :one
SELECT id FROM comments WHERE id = $1 AND post_id = $2 AND deleted_at IS NULL;

-- name: IncrementCommentCount :exec
UPDATE posts SET comment_count = comment_count + 1, updated_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetCommentByID :one
SELECT c.*, u.name AS author_name, u.handle AS author_handle
FROM comments c JOIN users u ON u.id = c.author_id
WHERE c.id = $1 AND c.deleted_at IS NULL;

-- name: ListComments :many
SELECT c.*, u.name AS author_name, u.handle AS author_handle
FROM comments c JOIN users u ON u.id = c.author_id
WHERE c.post_id = $1 AND c.deleted_at IS NULL
  AND ($2::timestamptz IS NULL OR (c.created_at, c.id) > ($2::timestamptz, $3::uuid))
ORDER BY c.created_at ASC, c.id ASC
LIMIT $4;

-- name: GetCommentParentInfo :one
-- 校验父评论属于同一帖子，并返回它自己的 parent_id，
-- 用于把回复层级限制在一层（PRD F-04「支持一级回复」）。
SELECT id, parent_id FROM comments
WHERE id = $1 AND post_id = $2 AND deleted_at IS NULL;
