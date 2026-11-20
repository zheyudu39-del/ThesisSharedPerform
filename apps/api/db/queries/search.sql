-- 检索模块查询（MVP：PostgreSQL tsvector + pg_trgm）。
--
-- 环境约定：PostgreSQL 18，未安装 pg_jieba / zhparser，分词统一使用 'simple' 配置：
--   * 英文与数字按空格、标点切分并转小写；
--   * 中文没有词边界，连续的一段汉字会成为「一个」词元，
--     因此「部分词」命中（例如用「深度」找「深度学习」）无法走 tsvector，
--     只能由 pg_trgm 的 ILIKE / similarity 兜底。
--
-- 排序规则：ts_rank（全文命中）优先，其次 similarity(title)（标题模糊命中），
-- 最后 updated_at 倒序。search_documents 没有 created_at，用 updated_at 作为时间兜底。

-- name: UpsertSearchDocument :exec
-- 按 (entity_type, entity_id) 幂等写入索引，并重算 tsv：
-- 标题权重 A、正文权重 B（与技术方案 §4.6 的「标题 > 摘要 > 正文」一致）。
INSERT INTO search_documents (entity_type, entity_id, title, body, tsv)
VALUES (
    sqlc.arg('entity_type')::varchar(16),
    sqlc.arg('entity_id')::uuid,
    sqlc.narg('title')::text,
    sqlc.narg('body')::text,
    setweight(to_tsvector('simple', COALESCE(sqlc.narg('title')::text, '')), 'A')
        || setweight(to_tsvector('simple', COALESCE(sqlc.narg('body')::text, '')), 'B')
)
ON CONFLICT (entity_type, entity_id) DO UPDATE SET
    title      = EXCLUDED.title,
    body       = EXCLUDED.body,
    tsv        = EXCLUDED.tsv,
    updated_at = now();

-- name: DeleteSearchDocument :exec
-- 删除一条索引；实体被删除或转为不可见时由调用方触发。
DELETE FROM search_documents
WHERE entity_type = sqlc.arg('entity_type')::varchar(16)
  AND entity_id = sqlc.arg('entity_id')::uuid;

-- name: SearchAll :many
-- 跨实体类型的统一检索。
--
-- 召回走三条通道（任一命中即可）：
--   1. tsvector 全文：websearch_to_tsquery 对任意输入都不会报错，空查询得到空 tsquery，
--      `tsv @@ 空查询` 恒为 false，天然安全；
--   2. 标题 ILIKE：命中 idx_search_title_trgm，覆盖中文部分词；
--   3. 正文 ILIKE：中文正文的部分词只能靠它（tsvector 无法切词）。
--      正文没有 trigram 索引，MVP 数据量下是可接受的顺序扫描；量级上来后
--      按技术方案迁移到 Meilisearch。
-- similarity(title, 原查询) 同时用于排序，作为「标题相近但未包含」的补充召回。
--
-- sqlc.arg('like_pattern') 由服务层拼好并转义（'%' || 转义后的关键词 || '%'），
-- 避免用户输入里的 % / _ 被当作通配符。
WITH q AS (
    SELECT
        sqlc.arg('query')::text AS raw,
        websearch_to_tsquery('simple', sqlc.arg('query')::text) AS tsq
)
SELECT
    sd.entity_type,
    sd.entity_id,
    COALESCE(sd.title, '')::text AS title,
    COALESCE(sd.body, '')::text  AS body,
    ts_rank(sd.tsv, q.tsq)::real AS rank,
    similarity(COALESCE(sd.title, ''), q.raw)::real AS title_similarity
FROM search_documents sd
CROSS JOIN q
-- 不包 COALESCE：title/body 为 NULL 时比较结果为 NULL（等价 false），
-- 同时保留 idx_search_title_trgm 的可用性（表达式索引无法被 COALESCE 命中）。
WHERE sd.tsv @@ q.tsq
   OR sd.title ILIKE sqlc.arg('like_pattern')::text
   OR sd.body ILIKE sqlc.arg('like_pattern')::text
   OR similarity(sd.title, q.raw) >= 0.2
ORDER BY rank DESC, title_similarity DESC, sd.updated_at DESC
LIMIT sqlc.arg('limit')::int;

-- name: SearchByTypes :many
-- 与 SearchAll 相同，但限定实体类型（types 为空数组时不会命中任何行，
-- 服务层在 types 为空时改走 SearchAll）。
WITH q AS (
    SELECT
        sqlc.arg('query')::text AS raw,
        websearch_to_tsquery('simple', sqlc.arg('query')::text) AS tsq
)
SELECT
    sd.entity_type,
    sd.entity_id,
    COALESCE(sd.title, '')::text AS title,
    COALESCE(sd.body, '')::text  AS body,
    ts_rank(sd.tsv, q.tsq)::real AS rank,
    similarity(COALESCE(sd.title, ''), q.raw)::real AS title_similarity
FROM search_documents sd
CROSS JOIN q
WHERE (
        sd.tsv @@ q.tsq
     OR sd.title ILIKE sqlc.arg('like_pattern')::text
     OR sd.body ILIKE sqlc.arg('like_pattern')::text
     OR similarity(sd.title, q.raw) >= 0.2
)
AND sd.entity_type = ANY(sqlc.arg('types')::text[])
ORDER BY rank DESC, title_similarity DESC, sd.updated_at DESC
LIMIT sqlc.arg('limit')::int;
