-- +goose Up
-- =====================================================================
-- 迁移 00002：精选策展
--
-- 产品背景（PRD §1.2、§5.1）：平台不做个性化推荐，用「人的判断」替代
-- 机器排序。精选流由运营/大V 策展，是冷启动期内容供给的主要手段。
--
-- 模型选择：以「策展集（collection）」为单位，而非给单条帖子打精选标记。
-- 理由：
--   1. 编辑可以按主题组织内容（如「本周必读 · 多模态」），天然带叙事；
--   2. 同一篇帖子可以同时出现在多个策展集里，不互斥；
--   3. 策展集有起止时间，便于做「本周精选」这类时效性栏目。
-- =====================================================================

-- 策展集：一个有标题、有编辑者、有上下架时间的集合。
CREATE TABLE collections (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    slug         varchar(64)  NOT NULL UNIQUE,
    title        varchar(128) NOT NULL,
    description  varchar(500),
    cover_key    text,
    -- 只有运营与大V 能创建（PRD §3 权限表：精选流准入与转发权重）
    curator_id   uuid NOT NULL REFERENCES users(id),
    visibility   post_visibility NOT NULL DEFAULT 'public',
    -- 下架时间早于 now() 时不再出现在列表与详情中；NULL 表示长期有效
    starts_at    timestamptz NOT NULL DEFAULT now(),
    ends_at      timestamptz,
    item_count   integer NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz,
    CHECK (ends_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX idx_collections_active ON collections (starts_at DESC, created_at DESC)
    WHERE deleted_at IS NULL AND visibility = 'public';
CREATE INDEX idx_collections_curator ON collections (curator_id, created_at DESC)
    WHERE deleted_at IS NULL;

-- 策展项：集合里的一条内容。
-- 目标可以是帖子（Post）或论文（Paper）——编辑既可以推荐「某人的观点」，
-- 也可以直接推荐「这篇论文本身」，后者对冷启动更有价值。
CREATE TABLE collection_items (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    collection_id uuid NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    post_id       uuid REFERENCES posts(id) ON DELETE CASCADE,
    paper_id      uuid REFERENCES papers(id) ON DELETE CASCADE,
    -- 编辑为这一条写的推荐语，展示在策展卡片上
    note          varchar(280),
    position      integer NOT NULL DEFAULT 0,
    added_by      uuid NOT NULL REFERENCES users(id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    -- 一个策展项必须指向帖子或论文之一，且不能同时指向两者
    CHECK ((post_id IS NOT NULL) <> (paper_id IS NOT NULL))
);

CREATE INDEX idx_collection_items_collection ON collection_items (collection_id, position, created_at);
-- 同一条内容在同一集合中只出现一次
CREATE UNIQUE INDEX uq_collection_items_post ON collection_items (collection_id, post_id)
    WHERE post_id IS NOT NULL;
CREATE UNIQUE INDEX uq_collection_items_paper ON collection_items (collection_id, paper_id)
    WHERE paper_id IS NOT NULL;

-- +goose StatementBegin
CREATE TRIGGER trg_collections_updated BEFORE UPDATE ON collections
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_collections_updated ON collections;
DROP TABLE IF EXISTS collection_items;
DROP TABLE IF EXISTS collections;
