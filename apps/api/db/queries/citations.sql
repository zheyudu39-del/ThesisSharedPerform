-- 文档引用与参考文献（PRD D-06）。
--
-- 约定：
--   * 引用挂在文档上，同一文档内 cite_key 唯一（见迁移 00004 的唯一约束）。
--   * 导入是幂等的：重复的 cite_key 更新书目字段而不是报错。用户反复粘贴
--     同一份 .bib 时不该产生重复条目，也不该得到「已存在」这种无意义错误。
--   * 权限判定不写进 SQL：由服务层复用 doc 包的纯函数，保证引用接口与文档
--     接口对「谁能看、谁能改」给出完全一致的结论。

-- name: GetDocumentAccessForReference :one
-- 一次性取回权限判定所需的全部事实，形状与 documents.sql 的
-- GetDocumentAccess 对齐（这里只多取了 default_role 需要的同名列）。
SELECT d.id AS doc_id, d.group_id, d.created_by,
       g.owner_id AS group_owner_id,
       COALESCE(gm.role::text, '') AS group_role,
       COALESCE(dm.role::text, '') AS doc_role,
       d.default_role::text AS default_role
FROM documents d
JOIN groups g ON g.id = d.group_id AND g.deleted_at IS NULL
LEFT JOIN group_members gm ON gm.group_id = d.group_id AND gm.user_id = sqlc.arg('user_id')
LEFT JOIN doc_members dm ON dm.doc_id = d.id AND dm.user_id = sqlc.arg('user_id')
WHERE d.id = sqlc.arg('doc_id') AND d.deleted_at IS NULL;

-- name: UpsertDocReference :one
-- 幂等导入的核心：冲突时以新导入的字段覆盖旧值。
-- paper_id / raw_bibtex 允许为 NULL，覆盖时用 COALESCE 保留原值，
-- 避免手工添加的条目被后续一次不含该字段的导入清空。
INSERT INTO doc_references (
    doc_id, paper_id, cite_key, entry_type, title, authors, year,
    container, volume, issue, pages, doi, url, publisher, raw_bibtex, created_by
) VALUES (
    sqlc.arg('doc_id'), sqlc.narg('paper_id'), sqlc.arg('cite_key'), sqlc.arg('entry_type'),
    sqlc.arg('title'), sqlc.arg('authors')::jsonb, sqlc.narg('year'),
    sqlc.narg('container'), sqlc.narg('volume'), sqlc.narg('issue'), sqlc.narg('pages'),
    sqlc.narg('doi'), sqlc.narg('url'), sqlc.narg('publisher'),
    sqlc.narg('raw_bibtex'), sqlc.narg('created_by')
)
ON CONFLICT (doc_id, cite_key) DO UPDATE SET
    paper_id   = COALESCE(EXCLUDED.paper_id, doc_references.paper_id),
    entry_type = EXCLUDED.entry_type,
    title      = EXCLUDED.title,
    authors    = EXCLUDED.authors,
    year       = EXCLUDED.year,
    container  = EXCLUDED.container,
    volume     = EXCLUDED.volume,
    issue      = EXCLUDED.issue,
    pages      = EXCLUDED.pages,
    doi        = EXCLUDED.doi,
    url        = EXCLUDED.url,
    publisher  = EXCLUDED.publisher,
    raw_bibtex = COALESCE(EXCLUDED.raw_bibtex, doc_references.raw_bibtex),
    updated_at = now()
RETURNING id, doc_id, paper_id, cite_key, entry_type, title, authors,
          COALESCE(year, 0) AS year,
          COALESCE(container, '') AS container,
          COALESCE(volume, '') AS volume,
          COALESCE(issue, '') AS issue,
          COALESCE(pages, '') AS pages,
          COALESCE(doi, '') AS doi,
          COALESCE(url, '') AS url,
          COALESCE(publisher, '') AS publisher,
          created_at, updated_at;

-- name: ListDocReferences :many
-- 按创建顺序返回：参考文献表由渲染层重新排序，这里只保证结果稳定。
SELECT id, doc_id, paper_id, cite_key, entry_type, title, authors,
       COALESCE(year, 0) AS year,
       COALESCE(container, '') AS container,
       COALESCE(volume, '') AS volume,
       COALESCE(issue, '') AS issue,
       COALESCE(pages, '') AS pages,
       COALESCE(doi, '') AS doi,
       COALESCE(url, '') AS url,
       COALESCE(publisher, '') AS publisher,
       created_at, updated_at
FROM doc_references
WHERE doc_id = sqlc.arg('doc_id')
ORDER BY created_at ASC, id ASC;

-- name: GetDocReference :one
SELECT id, doc_id, paper_id, cite_key, entry_type, title, authors,
       COALESCE(year, 0) AS year,
       COALESCE(container, '') AS container,
       COALESCE(volume, '') AS volume,
       COALESCE(issue, '') AS issue,
       COALESCE(pages, '') AS pages,
       COALESCE(doi, '') AS doi,
       COALESCE(url, '') AS url,
       COALESCE(publisher, '') AS publisher,
       created_at, updated_at
FROM doc_references
WHERE doc_id = sqlc.arg('doc_id') AND id = sqlc.arg('id');

-- name: DeleteDocReference :execrows
-- 带上 doc_id 条件：即使调用方拿到了别的文档的引用 ID 也删不掉。
DELETE FROM doc_references
WHERE doc_id = sqlc.arg('doc_id') AND id = sqlc.arg('id');
