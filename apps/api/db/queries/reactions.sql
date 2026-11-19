-- name: AddReaction :execrows
INSERT INTO reactions (user_id, target_type, target_id, kind)
VALUES ($1, 'post', $2, $3)
ON CONFLICT DO NOTHING;

-- name: RemoveReaction :execrows
DELETE FROM reactions
WHERE user_id = $1 AND target_type = 'post' AND target_id = $2 AND kind = $3;

-- name: AddLikeCount :one
UPDATE posts SET like_count = like_count + 1, updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING like_count;

-- name: RemoveLikeCount :one
UPDATE posts SET like_count = greatest(like_count - 1, 0), updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING like_count;

-- name: GetLikeCount :one
SELECT like_count FROM posts WHERE id = $1 AND deleted_at IS NULL;

-- name: HasReaction :one
SELECT EXISTS(SELECT 1 FROM reactions
WHERE user_id = $1 AND target_type = 'post' AND target_id = $2 AND kind = $3);
