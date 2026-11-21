-- 文档内嵌资源（图片）。

-- name: CreateDocAsset :one
-- 显式传入 id：storage_key 里已经嵌了这个 ID（doc-asset/{docId}/{assetId}），
-- 若让数据库自己生成，键里的 ID 与表里的 id 会对不上，读取时永远查不到。
INSERT INTO doc_assets (id, doc_id, storage_key, mime, size_bytes, original_name, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetDocAsset :one
SELECT * FROM doc_assets WHERE id = $1;

-- name: ListDocAssets :many
SELECT * FROM doc_assets WHERE doc_id = $1 ORDER BY created_at;
