-- +goose Up
-- =====================================================================
-- 迁移 00007：运营操作审计日志
--
-- 产品背景：A-05 要求「大V标识授予/收回，操作留审计日志」。
-- 后台已经有封禁、删帖、改角色等能力，但此前完全没有记录——出了问题
-- 无法回答「谁在什么时候封了谁」。这张表补齐这一环。
--
-- 设计取舍：
--   * 只记录**运营对他人数据的写操作**，不记普通用户的日常行为。
--     后者属于埋点，量级完全不同，混在一张表里会淹没审计信息。
--   * actor_id 用 ON DELETE SET NULL 而不是 CASCADE：运营账号被注销后，
--     历史记录仍然要留着。actor_handle 冗余存一份，保证账号删了也能追溯。
--   * target_id 不建外键：被删的帖子/评论/用户仍然要有审计记录，
--     外键会导致要么删不掉目标，要么连带删掉审计。
--   * detail 用 jsonb：不同动作的上下文差异很大（改角色记前后值、
--     封禁记原因），固定列会立刻不够用。
-- =====================================================================

CREATE TABLE admin_audit_logs (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    actor_id     uuid REFERENCES users(id) ON DELETE SET NULL,
    -- 冗余存一份句柄：actor 账号被删除后仍能回答「是谁做的」。
    actor_handle varchar(32) NOT NULL,
    -- 动作标识，如 user.role / user.status / post.delete / user.verify。
    action       varchar(64) NOT NULL,
    -- 目标类型与 ID。target_type 与 action 的前缀保持一致，便于按类型筛选。
    target_type  varchar(32) NOT NULL,
    target_id    uuid NOT NULL,
    -- 目标的可读标识（句柄、标题等），便于在列表里直接展示而不必回查。
    target_label varchar(255),
    -- 动作上下文：改角色记 before/after，封禁记原因。
    detail       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- 审计列表按时间倒序翻页，这是唯一的读模式。
CREATE INDEX idx_admin_audit_created ON admin_audit_logs (created_at DESC, id DESC);
-- 按操作人筛选（「这个运营最近做了什么」）。
CREATE INDEX idx_admin_audit_actor ON admin_audit_logs (actor_id, created_at DESC);
-- 按目标筛选（「这个用户被谁处理过」）。
CREATE INDEX idx_admin_audit_target ON admin_audit_logs (target_type, target_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS admin_audit_logs;
