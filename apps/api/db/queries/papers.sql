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

-- name: SetPaperPDFFile :one
-- 把一份已上传的文件挂到论文上。pdf_file_id 为空表示卸载全文。
UPDATE papers
SET pdf_file_id = sqlc.narg('pdf_file_id')
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: UpdatePaperMetadata :one
-- 元数据局部更新（PRD P-02）：参数为 NULL 表示「本次不修改」。
-- 用 COALESCE 在单条语句内完成，避免读-改-写之间的并发覆盖。
--
-- title_norm 与 dedupe_hash 必须跟着标题/作者/年份一起重算，
-- 否则改了标题之后软去重会失效（旧哈希指向旧标题）。
-- 这两项由服务层算好后传入；传 NULL 表示标题相关字段没变。
UPDATE papers SET
    title           = COALESCE(sqlc.narg('title')::text, title),
    title_norm      = COALESCE(sqlc.narg('title_norm')::varchar(512), title_norm),
    authors         = COALESCE(sqlc.narg('authors')::jsonb, authors),
    abstract        = COALESCE(sqlc.narg('abstract')::text, abstract),
    venue           = COALESCE(sqlc.narg('venue')::varchar(256), venue),
    published_at    = COALESCE(sqlc.narg('published_at')::date, published_at),
    citation_count  = COALESCE(sqlc.narg('citation_count')::integer, citation_count),
    dedupe_hash     = COALESCE(sqlc.narg('dedupe_hash')::char(64), dedupe_hash)
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: FindPaperDuplicate :one
-- 合并前查重：标题归一化 + 第一作者 + 年份相同的另一篇论文。
-- 用于「两篇重复记录合并成一篇」的场景（PRD P-02）。
SELECT id, title FROM papers
WHERE id <> sqlc.arg('exclude_id')::uuid
  AND title_norm = sqlc.arg('title_norm')::varchar(512)
LIMIT 1;

-- name: MergePaperPosts :execrows
-- 合并：把重复论文下的帖子挂到保留的论文上。
UPDATE posts SET paper_id = sqlc.arg('keep_id')::uuid
WHERE paper_id = sqlc.arg('dup_id')::uuid;

-- name: MergePaperTags :execrows
-- 合并：迁移标签，跳过保留方已有的（paper_tags 主键是 (paper_id, tag_id)）。
INSERT INTO paper_tags (paper_id, tag_id)
SELECT sqlc.arg('keep_id')::uuid, tag_id
  FROM paper_tags WHERE paper_id = sqlc.arg('dup_id')::uuid
ON CONFLICT DO NOTHING;

-- name: MergePaperLibraryItems :execrows
-- 合并：群资料库里的条目改挂到保留的论文。
UPDATE group_library_items SET paper_id = sqlc.arg('keep_id')::uuid
WHERE paper_id = sqlc.arg('dup_id')::uuid;

-- name: MergePaperCollectionItems :execrows
-- 合并：策展项改挂到保留的论文。
UPDATE collection_items SET paper_id = sqlc.arg('keep_id')::uuid
WHERE paper_id = sqlc.arg('dup_id')::uuid;

-- name: MergePaperDocReferences :execrows
-- 合并：文档引用改指向保留的论文。
UPDATE doc_references SET paper_id = sqlc.arg('keep_id')::uuid
WHERE paper_id = sqlc.arg('dup_id')::uuid;

-- name: DeletePaper :exec
-- 合并完成后删除重复记录。此时已没有外键指向它。
DELETE FROM papers WHERE id = sqlc.arg('id')::uuid;

-- name: ClearPaperDedupeHash :exec
-- 合并前把重复方的软去重哈希清空：两篇论文归一化标题相同，
-- 哈希也相同，不先清掉会撞 dedupe_hash 的唯一索引。
UPDATE papers SET dedupe_hash = NULL WHERE id = sqlc.arg('id')::uuid;
