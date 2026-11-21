-- 开放 API 密钥查询。

-- name: CreateAPIKey :one
INSERT INTO api_keys (user_id, name, key_hash, key_prefix, scopes, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListAPIKeys :many
-- 只列出未撤销的密钥。明文早已不可得，前端展示 key_prefix 供识别。
SELECT * FROM api_keys
WHERE user_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: GetAPIKeyByHash :one
-- 认证路径：哈希命中、未撤销、未过期三者同时满足才算有效。
-- 三个条件写进同一条语句，避免调用方漏判其中任何一个。
SELECT * FROM api_keys
WHERE key_hash = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now())
LIMIT 1;

-- name: RevokeAPIKey :execrows
-- 带 user_id 条件：撤销他人密钥时影响 0 行，服务层据此返回 404。
UPDATE api_keys SET revoked_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: TouchAPIKey :exec
-- 记录最近使用时间。失败不影响请求，仅用于界面上展示「最后使用」。
UPDATE api_keys SET last_used_at = now() WHERE id = $1;
