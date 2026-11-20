-- 关注关系：follows 表以 (follower_id, followee_id) 为主键，
-- 因此写入/删除天然幂等，计数一律从表内 COUNT(*) 实时统计，
-- 不维护冗余列，避免并发下计数漂移。

-- name: FollowUser :execrows
INSERT INTO follows (follower_id, followee_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: UnfollowUser :execrows
DELETE FROM follows
WHERE follower_id = $1 AND followee_id = $2;

-- name: CountFollowers :one
-- 被多少人关注。
SELECT COUNT(*) FROM follows WHERE followee_id = $1;

-- name: CountFollowing :one
-- 关注了多少人。
SELECT COUNT(*) FROM follows WHERE follower_id = $1;

-- name: IsFollowing :one
SELECT EXISTS(
    SELECT 1 FROM follows WHERE follower_id = $1 AND followee_id = $2
);
