-- name: CreateUser :one
INSERT INTO users (email, password_hash, name, handle)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: GetUserByHandle :one
SELECT * FROM users
WHERE handle = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: GetPublicUserByHandle :one
-- 公开投影：只选择可以对外展示的列。
-- 绝不包含 email / password_hash / role / status 等私密字段，
-- 避免个人主页接口意外泄露账号信息。
SELECT id, name, handle, bio, institution, research_fields, verify_type, created_at
FROM users
WHERE handle = $1 AND deleted_at IS NULL AND status = 'active'
LIMIT 1;

-- name: CountPostsByAuthor :one
-- 个人主页的帖子数：只统计未删除的帖子（含转发），与「发帖 Tab」口径一致。
SELECT COUNT(*) FROM posts
WHERE author_id = $1 AND deleted_at IS NULL;

-- name: UpdateUserProfile :one
-- 局部更新：参数为 NULL 表示「本次不修改」，空字符串/空数组表示「清空」。
-- 用 COALESCE 在单条语句内完成，避免读-改-写之间的并发覆盖。
UPDATE users SET
    name            = COALESCE(sqlc.narg('name')::varchar(64), name),
    bio             = COALESCE(sqlc.narg('bio')::varchar(280), bio),
    institution     = COALESCE(sqlc.narg('institution')::varchar(128), institution),
    research_fields = COALESCE(sqlc.narg('research_fields')::text[], research_fields)
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;
