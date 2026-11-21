-- +goose Up
-- =====================================================================
-- 迁移 00003：开放 API 密钥
--
-- 产品背景（PRD M4「开放 API」）：让第三方程序以只读方式访问公开数据，
-- 支撑文献聚合、个人站点嵌入等场景。
--
-- 安全设计：
--   1. 只存 SHA-256 哈希，不存明文 —— 与密码同理，库被拖走也拿不到可用凭据；
--   2. 保留 key_prefix 用于在界面上识别「这是哪一把钥匙」，
--      前缀长度不足以暴力还原完整 Key；
--   3. 撤销用 revoked_at 软删除，保留审计痕迹；
--   4. scopes 是白名单，越权访问返回 403 而不是静默降级。
-- =====================================================================

CREATE TABLE api_keys (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         varchar(64) NOT NULL,
    -- SHA-256 十六进制串固定 64 字符。Key 本身是 32 字节随机数，
    -- 熵足够高，不需要加盐或慢哈希（不同于用户口令）。
    key_hash     char(64) NOT NULL UNIQUE,
    -- 展示用前缀：形如 "tsk_ab12cd34"，让用户能在列表里认出是哪把钥匙。
    key_prefix   varchar(12) NOT NULL,
    scopes       text[] NOT NULL DEFAULT '{}',
    last_used_at timestamptz,
    expires_at   timestamptz,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- 只索引未撤销的密钥：列表接口只关心这些，撤销后的行不进索引。
CREATE INDEX idx_api_keys_user ON api_keys (user_id, created_at DESC)
    WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS api_keys;
