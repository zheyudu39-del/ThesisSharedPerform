-- name: CreatePaper :one
INSERT INTO papers (
    doi, arxiv_id, openreview_id, title, title_norm, authors, abstract,
    venue, published_at, citation_count, meta_source, meta_fetched_at,
    dedupe_hash, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
RETURNING *;

-- name: GetPaperByID :one
SELECT * FROM papers WHERE id = $1;

-- name: GetPaperByDOI :one
SELECT * FROM papers WHERE doi = $1;

-- name: GetPaperByArxivID :one
SELECT * FROM papers WHERE arxiv_id = $1;

-- name: GetPaperByDedupeHash :one
SELECT * FROM papers WHERE dedupe_hash = $1;

-- name: ListPapers :many
SELECT * FROM papers
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountPapers :one
SELECT count(*) FROM papers;
