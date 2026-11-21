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

-- name: SearchFiltered :many
-- 带筛选条件的检索（S-03）：时间范围 / 标签 / 作者。
--
-- 这些条件依赖实体自身的列，search_documents 没有，因此按 entity_type
-- 分别 LEFT JOIN 回源表：
--   * post  → posts（created_at、author_id；标签经 post_tags）
--   * paper → papers（created_at；标签经 paper_tags；论文没有作者 handle）
--   * user / group → 没有标签与作者列，带 tag / author 时被排除。
--
-- 用 LEFT JOIN 而不是 INNER JOIN：一个筛选条件只对某类实体有意义，
-- INNER JOIN 会连带删掉不满足该条件的其它整类实体，而不是「按规则排除」。
-- 时间范围对 post / paper 用各自的 created_at；其它实体没有 created_at，
-- 退回索引的 updated_at（与 SearchAll 的时间兜底一致）。
--
-- types 传 NULL 表示不限类型；tag / author 传 NULL 表示不过滤。
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
LEFT JOIN posts p  ON sd.entity_type = 'post'  AND p.id = sd.entity_id AND p.deleted_at IS NULL
LEFT JOIN papers pa ON sd.entity_type = 'paper' AND pa.id = sd.entity_id
WHERE (
        sd.tsv @@ q.tsq
     OR sd.title ILIKE sqlc.arg('like_pattern')::text
     OR sd.body ILIKE sqlc.arg('like_pattern')::text
     OR similarity(sd.title, q.raw) >= 0.2
)
AND (
    sqlc.narg('types')::text[] IS NULL
    OR sd.entity_type = ANY(sqlc.narg('types')::text[])
)
AND (
    (sqlc.narg('from_time')::timestamptz IS NULL AND sqlc.narg('to_time')::timestamptz IS NULL)
    OR (sd.entity_type = 'post' AND p.id IS NOT NULL
        AND (sqlc.narg('from_time')::timestamptz IS NULL OR p.created_at >= sqlc.narg('from_time')::timestamptz)
        AND (sqlc.narg('to_time')::timestamptz IS NULL OR p.created_at <= sqlc.narg('to_time')::timestamptz))
    OR (sd.entity_type = 'paper' AND pa.id IS NOT NULL
        AND (sqlc.narg('from_time')::timestamptz IS NULL OR pa.created_at >= sqlc.narg('from_time')::timestamptz)
        AND (sqlc.narg('to_time')::timestamptz IS NULL OR pa.created_at <= sqlc.narg('to_time')::timestamptz))
    OR (sd.entity_type NOT IN ('post', 'paper')
        AND sd.updated_at >= COALESCE(sqlc.narg('from_time')::timestamptz, sd.updated_at)
        AND sd.updated_at <= COALESCE(sqlc.narg('to_time')::timestamptz, sd.updated_at))
)
AND (
    sqlc.narg('tag')::text IS NULL
    OR (sd.entity_type = 'post' AND p.id IS NOT NULL AND EXISTS(
          SELECT 1 FROM post_tags pt JOIN tags t ON t.id = pt.tag_id
          WHERE pt.post_id = p.id AND t.slug = sqlc.narg('tag')::text))
    OR (sd.entity_type = 'paper' AND pa.id IS NOT NULL AND EXISTS(
          SELECT 1 FROM paper_tags pt JOIN tags t ON t.id = pt.tag_id
          WHERE pt.paper_id = pa.id AND t.slug = sqlc.narg('tag')::text))
)
AND (
    sqlc.narg('author')::text IS NULL
    OR (sd.entity_type = 'post' AND p.id IS NOT NULL AND EXISTS(
          SELECT 1 FROM users u WHERE u.id = p.author_id AND lower(u.handle) = sqlc.narg('author')::text))
)
ORDER BY rank DESC, title_similarity DESC, sd.updated_at DESC
LIMIT sqlc.arg('limit')::int;
