-- +goose Up
-- =====================================================================
-- 迁移 00005：群文件库
--
-- 产品背景（PRD G-05）：群内共享文献，按标签分类，
-- 并支持「从群内 Post 一键存入文件库」。
--
-- 模型选择：群文件库是「指向已有文件的引用」，而不是文件副本。
-- 理由：
--   1. 文件本体在对象存储里，复制一份既浪费空间又要处理生命周期；
--   2. 同一篇 PDF 可能同时属于多个群，副本会让「谁上传的」变得模糊；
--   3. 帖子里的文件可以零成本存入文件库，符合「一键存入」的产品描述。
-- =====================================================================

CREATE TABLE group_files (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    group_id    uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    file_id     uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    -- 来源帖子（可选）：从群内 Post 存入时记录，便于回溯上下文
    post_id     uuid REFERENCES posts(id) ON DELETE SET NULL,
    title       varchar(255) NOT NULL,
    description varchar(500),
    tags        text[] NOT NULL DEFAULT '{}',
    added_by    uuid NOT NULL REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- 同一个文件在同一个群里只登记一次
    UNIQUE (group_id, file_id)
);

CREATE INDEX idx_group_files_group ON group_files (group_id, created_at DESC);
-- 标签检索：GIN 支持 tags @> ARRAY['x'] 这样的包含查询
CREATE INDEX idx_group_files_tags ON group_files USING gin (tags);

-- +goose Down
DROP TABLE IF EXISTS group_files;
