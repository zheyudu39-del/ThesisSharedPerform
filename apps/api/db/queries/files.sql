-- name: CreateFile :one
INSERT INTO files (
    id, owner_id, storage_key, original_name, mime, kind, size_bytes, status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, 'pending'
)
RETURNING *;

-- name: GetFileByID :one
SELECT * FROM files WHERE id = $1;

-- name: UpdateFileStatus :one
UPDATE files
SET status       = $2,
    error_reason = $3,
    page_count   = COALESCE(sqlc.narg('page_count')::integer, page_count),
    cover_key    = COALESCE(sqlc.narg('cover_key'), cover_key),
    ready_at     = CASE WHEN $2 = 'ready' THEN now() ELSE ready_at END
WHERE id = $1
RETURNING *;

-- name: MarkFileUploaded :one
-- 幂等：只有 pending / uploading 能进入 uploaded；重复调用返回 0 行。
UPDATE files
SET status     = 'uploaded',
    size_bytes = $2
WHERE id = $1 AND status IN ('pending', 'uploading')
RETURNING *;

-- name: MarkFileTranscoding :one
UPDATE files
SET status       = 'transcoding',
    error_reason = NULL
WHERE id = $1 AND status IN ('uploaded', 'failed')
RETURNING *;

-- name: MarkFileReady :one
UPDATE files
SET status    = 'ready',
    page_count  = $2,
    cover_key   = $3,
    ready_at    = now()
WHERE id = $1 AND status = 'transcoding'
RETURNING *;

-- name: MarkFileFailed :one
UPDATE files
SET status       = 'failed',
    error_reason = $2,
    retry_count  = retry_count + 1
WHERE id = $1 AND status = 'transcoding'
RETURNING *;

-- name: InsertFilePage :exec
INSERT INTO file_pages (file_id, page_no, image_key, width, height, text_chars)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (file_id, page_no) DO UPDATE
SET image_key = EXCLUDED.image_key,
    width     = EXCLUDED.width,
    height    = EXCLUDED.height;

-- name: ListFilePages :many
SELECT * FROM file_pages
WHERE file_id = $1
ORDER BY page_no
LIMIT $2 OFFSET $3;

-- name: CountFilePages :one
SELECT count(*) FROM file_pages WHERE file_id = $1;
