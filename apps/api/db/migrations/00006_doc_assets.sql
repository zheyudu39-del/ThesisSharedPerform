-- +goose Up
-- =====================================================================
-- 迁移 00006：文档内嵌资源（图片）
--
-- 产品背景：导入 Word / LaTeX 时，文档里的图片不能丢。
-- 图片必须能让「所有能看这篇文档的人」看到，而不是只有上传者本人——
-- 这与 files 表的模型（owner 独占）不同，因此单独建表。
--
-- 权限模型：访问图片时校验「当前用户能否查看所属文档」，
-- 复用 doc 包已有的 EffectiveRole 判定，不重复实现一套。
-- =====================================================================

CREATE TABLE doc_assets (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    doc_id      uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    -- 对象存储键。图片不经过 files 表，避免与「个人文件」的权限语义混淆。
    storage_key text NOT NULL,
    mime        varchar(128) NOT NULL,
    size_bytes  bigint NOT NULL,
    -- 原始文件名，仅用于展示与调试
    original_name varchar(255),
    created_by  uuid REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_doc_assets_doc ON doc_assets (doc_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS doc_assets;
