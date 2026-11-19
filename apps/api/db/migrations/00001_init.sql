-- +goose Up
-- =====================================================================
-- 迁移 00001：初始 schema
-- 来源：docs/data-model.md（v0.1）
-- 说明：UUIDv7 主键、软删除、计数冗余、权限外置，见数据模型文档。
-- =====================================================================

-- ---------------------------------------------------------------------
-- 扩展
-- ---------------------------------------------------------------------
CREATE EXTENSION IF NOT EXISTS pgcrypto;   -- gen_random_bytes / gen_random_uuid
CREATE EXTENSION IF NOT EXISTS citext;     -- 大小写不敏感的 email
CREATE EXTENSION IF NOT EXISTS pg_trgm;    -- 模糊匹配（检索兜底）

-- ---------------------------------------------------------------------
-- UUIDv7 生成函数（有序、不可枚举）。时间 48bit + version 7 + variant 10 + 随机。
-- 注意：随机部分来自 random()，用于主键的有序性与防碰撞，非加密用途。
-- ---------------------------------------------------------------------
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION uuidv7() RETURNS uuid AS $$
DECLARE
    ts_ms   bigint;
    t_hi    bigint;
    t_lo    bigint;
    rand_hi bigint;
    rand_lo bigint;
BEGIN
    ts_ms := (extract(epoch FROM clock_timestamp()) * 1000)::bigint;

    t_hi := (ts_ms >> 16) & 4294967295;             -- 时间戳 bits 16..47
    t_lo := ts_ms & 65535;                          -- 时间戳 bits 0..15

    rand_hi := floor(random() * 4096)::bigint;      -- 12 bit 随机
    rand_lo := floor(random() * 4611686018427387904)::bigint; -- 62 bit 随机

    RETURN (
        lpad(to_hex(t_hi), 8, '0') ||
        lpad(to_hex((t_lo << 16) | (7::bigint << 12) | rand_hi), 8, '0') ||
        lpad(to_hex((2::bigint << 62) | rand_lo), 16, '0')
    )::uuid;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- ---------------------------------------------------------------------
-- 枚举类型（12 个，见 data-model.md §3）
-- ---------------------------------------------------------------------
CREATE TYPE user_role        AS ENUM ('user', 'admin');
CREATE TYPE verify_type      AS ENUM ('none', 'scholar', 'influencer');
CREATE TYPE post_type        AS ENUM ('original', 'repost');
CREATE TYPE post_visibility  AS ENUM ('public', 'followers', 'group', 'private');
CREATE TYPE file_status      AS ENUM ('pending', 'uploading', 'uploaded',
                                      'scanning', 'transcoding', 'ready',
                                      'rejected', 'failed');
CREATE TYPE file_kind        AS ENUM ('pdf', 'office_doc', 'office_slide',
                                      'office_sheet', 'image', 'markdown',
                                      'code', 'latex', 'video', 'archive', 'other');
CREATE TYPE group_join_policy AS ENUM ('public', 'request', 'invite');
CREATE TYPE group_role       AS ENUM ('owner', 'admin', 'member');
CREATE TYPE doc_role         AS ENUM ('owner', 'editor', 'viewer');
CREATE TYPE doc_status       AS ENUM ('draft', 'active', 'archived');
CREATE TYPE notify_type      AS ENUM ('comment', 'reply', 'mention', 'repost',
                                      'like_agg', 'join_request', 'doc_edit',
                                      'system');
CREATE TYPE report_status    AS ENUM ('pending', 'reviewing', 'resolved', 'rejected');

-- ---------------------------------------------------------------------
-- 4.1 用户与关系
-- ---------------------------------------------------------------------
CREATE TABLE users (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    email             citext UNIQUE,
    email_verified_at timestamptz,
    password_hash     text,
    name              varchar(64)  NOT NULL,
    handle            varchar(32)  NOT NULL UNIQUE,
    avatar_key        text,
    bio               varchar(280),
    institution       varchar(128),
    research_fields   text[]       NOT NULL DEFAULT '{}',
    verify_type       verify_type  NOT NULL DEFAULT 'none',
    verify_org        varchar(128),
    role              user_role    NOT NULL DEFAULT 'user',
    status            varchar(16)  NOT NULL DEFAULT 'active',
    last_active_at    timestamptz,
    created_at        timestamptz  NOT NULL DEFAULT now(),
    updated_at        timestamptz  NOT NULL DEFAULT now(),
    deleted_at        timestamptz
);

CREATE TABLE user_oauth_accounts (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider     varchar(32) NOT NULL,
    provider_uid varchar(128) NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_uid)
);

CREATE TABLE follows (
    follower_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    followee_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (follower_id, followee_id),
    CHECK (follower_id <> followee_id)
);

CREATE INDEX idx_users_handle ON users (handle);
CREATE INDEX idx_follows_followee ON follows (followee_id, created_at DESC);

-- ---------------------------------------------------------------------
-- 4.2 论文实体
-- ---------------------------------------------------------------------
CREATE TABLE papers (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    doi             varchar(128) UNIQUE,
    arxiv_id        varchar(32) UNIQUE,
    openreview_id   varchar(64),
    title           text NOT NULL,
    title_norm      varchar(512) NOT NULL,
    authors         jsonb NOT NULL DEFAULT '[]',
    abstract        text,
    venue           varchar(256),
    published_at    date,
    citation_count  integer NOT NULL DEFAULT 0,
    pdf_file_id     uuid,
    meta_source     varchar(32),
    meta_fetched_at timestamptz,
    dedupe_hash     char(64) UNIQUE,
    created_by      uuid REFERENCES users(id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
-- 注意：pdf_file_id 对 files(id) 的外键在 files 表创建后追加（见下方 ALTER）。

-- ---------------------------------------------------------------------
-- 4.3 帖子与互动
-- ---------------------------------------------------------------------
CREATE TABLE posts (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    author_id      uuid NOT NULL REFERENCES users(id),
    type           post_type NOT NULL DEFAULT 'original',
    content_html   text,
    content_text   text,
    paper_id       uuid REFERENCES papers(id),
    repost_of      uuid REFERENCES posts(id),
    group_id       uuid,
    visibility     post_visibility NOT NULL DEFAULT 'public',
    like_count     integer NOT NULL DEFAULT 0,
    comment_count  integer NOT NULL DEFAULT 0,
    repost_count   integer NOT NULL DEFAULT 0,
    hot_score      real NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    deleted_at     timestamptz,
    CHECK (type <> 'repost' OR repost_of IS NOT NULL)
);

CREATE INDEX idx_posts_feed_all ON posts (created_at DESC, id DESC)
    WHERE deleted_at IS NULL AND visibility = 'public' AND group_id IS NULL;
CREATE INDEX idx_posts_author ON posts (author_id, created_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_posts_paper ON posts (paper_id, created_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_posts_group ON posts (group_id, created_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_posts_hot ON posts (hot_score DESC, created_at DESC)
    WHERE deleted_at IS NULL AND visibility = 'public';

CREATE TABLE post_attachments (
    id       uuid PRIMARY KEY DEFAULT uuidv7(),
    post_id  uuid NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    file_id  uuid NOT NULL,
    position smallint NOT NULL DEFAULT 0,
    UNIQUE (post_id, file_id)
);

CREATE TABLE comments (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    post_id      uuid NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    parent_id    uuid REFERENCES comments(id) ON DELETE CASCADE,
    author_id    uuid NOT NULL REFERENCES users(id),
    content_html text NOT NULL,
    content_text text NOT NULL,
    like_count   integer NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz
);
CREATE INDEX idx_comments_post ON comments (post_id, created_at);

CREATE TABLE reactions (
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type varchar(16) NOT NULL,
    target_id   uuid NOT NULL,
    kind        varchar(16) NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, target_type, target_id, kind)
);
CREATE INDEX idx_reactions_target ON reactions (target_type, target_id, kind);

-- ---------------------------------------------------------------------
-- 4.4 文件与预览
-- ---------------------------------------------------------------------
CREATE TABLE files (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    owner_id       uuid NOT NULL REFERENCES users(id),
    storage_key    text NOT NULL,
    original_name  varchar(255) NOT NULL,
    mime           varchar(128) NOT NULL,
    kind           file_kind NOT NULL,
    size_bytes     bigint NOT NULL,
    sha256         char(64),
    status         file_status NOT NULL DEFAULT 'pending',
    scan_result    varchar(32),
    page_count     integer,
    cover_key      text,
    text_layer_key text,
    error_reason   text,
    retry_count    smallint NOT NULL DEFAULT 0,
    visibility     post_visibility NOT NULL DEFAULT 'public',
    created_at     timestamptz NOT NULL DEFAULT now(),
    ready_at       timestamptz
);

CREATE INDEX idx_files_owner  ON files (owner_id, created_at DESC);
CREATE INDEX idx_files_status ON files (status) WHERE status NOT IN ('ready', 'rejected');
CREATE UNIQUE INDEX uq_files_sha ON files (owner_id, sha256) WHERE sha256 IS NOT NULL;

CREATE TABLE file_pages (
    file_id    uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    page_no    integer NOT NULL,
    image_key  text NOT NULL,
    width      integer NOT NULL,
    height     integer NOT NULL,
    text_chars integer NOT NULL DEFAULT 0,
    PRIMARY KEY (file_id, page_no)
);

-- ---------------------------------------------------------------------
-- 4.5 群组
-- ---------------------------------------------------------------------
CREATE TABLE groups (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    name         varchar(64) NOT NULL,
    slug         varchar(64) UNIQUE,
    description  varchar(500),
    cover_key    text,
    owner_id     uuid NOT NULL REFERENCES users(id),
    join_policy  group_join_policy NOT NULL DEFAULT 'invite',
    member_count integer NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz
);

CREATE TABLE group_members (
    group_id  uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role      group_role NOT NULL DEFAULT 'member',
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX idx_group_members_user ON group_members (user_id);

CREATE TABLE group_invites (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    group_id   uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    code       varchar(32) NOT NULL UNIQUE,
    created_by uuid NOT NULL REFERENCES users(id),
    expires_at timestamptz,
    max_uses   integer,
    used_count integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE join_requests (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    group_id    uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message     varchar(280),
    status      report_status NOT NULL DEFAULT 'pending',
    reviewed_by uuid REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (group_id, user_id)
);

CREATE TABLE group_library_items (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    group_id   uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    paper_id   uuid REFERENCES papers(id),
    file_id    uuid REFERENCES files(id),
    added_by   uuid NOT NULL REFERENCES users(id),
    note       varchar(500),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (paper_id IS NOT NULL OR file_id IS NOT NULL)
);
CREATE INDEX idx_group_library ON group_library_items (group_id, created_at DESC);

-- ---------------------------------------------------------------------
-- 4.6 论文文档与协同
-- ---------------------------------------------------------------------
CREATE TABLE documents (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    group_id          uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    title             varchar(256) NOT NULL,
    status            doc_status NOT NULL DEFAULT 'draft',
    default_role      doc_role NOT NULL DEFAULT 'editor',
    latest_snapshot   integer NOT NULL DEFAULT 0,
    latest_update_seq bigint NOT NULL DEFAULT 0,
    word_count        integer NOT NULL DEFAULT 0,
    created_by        uuid NOT NULL REFERENCES users(id),
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz
);
CREATE INDEX idx_documents_group ON documents (group_id, updated_at DESC);

CREATE TABLE doc_members (
    doc_id     uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       doc_role NOT NULL,
    granted_by uuid REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (doc_id, user_id)
);

CREATE TABLE doc_snapshots (
    doc_id      uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    version     integer NOT NULL,
    storage_key text NOT NULL,
    size_bytes  bigint NOT NULL,
    label       varchar(64),
    created_by  uuid REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (doc_id, version)
);

CREATE TABLE doc_updates (
    doc_id      uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    seq         bigint NOT NULL,
    storage_key text NOT NULL,
    size_bytes  integer NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (doc_id, seq)
);

-- ---------------------------------------------------------------------
-- 4.7 批注
-- ---------------------------------------------------------------------
CREATE TABLE annotations (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    target_type varchar(16) NOT NULL,
    target_id   uuid NOT NULL,
    author_id   uuid NOT NULL REFERENCES users(id),
    anchor      jsonb NOT NULL,
    quoted_text text,
    content     text NOT NULL,
    resolved_at timestamptz,
    resolved_by uuid REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);
CREATE INDEX idx_annotations_target ON annotations (target_type, target_id, created_at);

-- ---------------------------------------------------------------------
-- 4.8 标签
-- ---------------------------------------------------------------------
CREATE TABLE tags (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    name       varchar(64) NOT NULL,
    slug       varchar(64) NOT NULL UNIQUE,
    use_count  integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE post_tags (
    post_id uuid NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    tag_id  uuid NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (post_id, tag_id)
);
CREATE INDEX idx_post_tags_tag ON post_tags (tag_id);

CREATE TABLE paper_tags (
    paper_id uuid NOT NULL REFERENCES papers(id) ON DELETE CASCADE,
    tag_id   uuid NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (paper_id, tag_id)
);

-- ---------------------------------------------------------------------
-- 4.9 通知与审核
-- ---------------------------------------------------------------------
CREATE TABLE notifications (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type        notify_type NOT NULL,
    actor_id    uuid REFERENCES users(id),
    target_type varchar(16),
    target_id   uuid,
    payload     jsonb NOT NULL DEFAULT '{}',
    read_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_notifications_unread ON notifications (user_id, created_at DESC)
    WHERE read_at IS NULL;

CREATE TABLE notification_prefs (
    user_id      uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    muted_types  notify_type[] NOT NULL DEFAULT '{}',
    email_digest varchar(16) NOT NULL DEFAULT 'daily'
);

CREATE TABLE reports (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    reporter_id uuid NOT NULL REFERENCES users(id),
    target_type varchar(16) NOT NULL,
    target_id   uuid NOT NULL,
    reason      varchar(64) NOT NULL,
    detail      varchar(500),
    status      report_status NOT NULL DEFAULT 'pending',
    handled_by  uuid REFERENCES users(id),
    handled_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_reports_pending ON reports (created_at) WHERE status = 'pending';

CREATE TABLE blocks (
    blocker_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id)
);

-- ---------------------------------------------------------------------
-- 4.10 检索（MVP 方案）
-- ---------------------------------------------------------------------
CREATE TABLE search_documents (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    entity_type varchar(16) NOT NULL,
    entity_id   uuid NOT NULL,
    title       text,
    body        text,
    lang        varchar(8) NOT NULL DEFAULT 'zh',
    tsv         tsvector NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entity_type, entity_id)
);
CREATE INDEX idx_search_tsv ON search_documents USING gin (tsv);
CREATE INDEX idx_search_title_trgm ON search_documents USING gin (title gin_trgm_ops);

-- ---------------------------------------------------------------------
-- 循环外键（数据模型文档为简化省略，这里补全以保引用完整性）
-- ---------------------------------------------------------------------
ALTER TABLE papers
    ADD CONSTRAINT fk_papers_pdf_file FOREIGN KEY (pdf_file_id) REFERENCES files(id) ON DELETE SET NULL;

ALTER TABLE posts
    ADD CONSTRAINT fk_posts_group FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE CASCADE;

ALTER TABLE post_attachments
    ADD CONSTRAINT fk_post_attachments_file FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE;

-- ---------------------------------------------------------------------
-- 触发器：updated_at 自动维护
-- ---------------------------------------------------------------------
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_users_updated   BEFORE UPDATE ON users   FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_papers_updated  BEFORE UPDATE ON papers  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_posts_updated   BEFORE UPDATE ON posts   FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_documents_updated BEFORE UPDATE ON documents FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS trg_documents_updated ON documents;
DROP TRIGGER IF EXISTS trg_posts_updated ON posts;
DROP TRIGGER IF EXISTS trg_papers_updated ON papers;
DROP TRIGGER IF EXISTS trg_users_updated ON users;
DROP FUNCTION IF EXISTS set_updated_at();

ALTER TABLE post_attachments DROP CONSTRAINT IF EXISTS fk_post_attachments_file;
ALTER TABLE posts DROP CONSTRAINT IF EXISTS fk_posts_group;
ALTER TABLE papers DROP CONSTRAINT IF EXISTS fk_papers_pdf_file;

DROP TABLE IF EXISTS search_documents;
DROP TABLE IF EXISTS blocks;
DROP TABLE IF EXISTS reports;
DROP TABLE IF EXISTS notification_prefs;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS paper_tags;
DROP TABLE IF EXISTS post_tags;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS annotations;
DROP TABLE IF EXISTS doc_updates;
DROP TABLE IF EXISTS doc_snapshots;
DROP TABLE IF EXISTS doc_members;
DROP TABLE IF EXISTS documents;
DROP TABLE IF EXISTS group_library_items;
DROP TABLE IF EXISTS join_requests;
DROP TABLE IF EXISTS group_invites;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS file_pages;
DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS reactions;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS post_attachments;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS papers;
DROP TABLE IF EXISTS follows;
DROP TABLE IF EXISTS user_oauth_accounts;
DROP TABLE IF EXISTS users;

DROP TYPE IF EXISTS report_status;
DROP TYPE IF EXISTS notify_type;
DROP TYPE IF EXISTS doc_status;
DROP TYPE IF EXISTS doc_role;
DROP TYPE IF EXISTS group_role;
DROP TYPE IF EXISTS group_join_policy;
DROP TYPE IF EXISTS file_kind;
DROP TYPE IF EXISTS file_status;
DROP TYPE IF EXISTS post_visibility;
DROP TYPE IF EXISTS post_type;
DROP TYPE IF EXISTS verify_type;
DROP TYPE IF EXISTS user_role;

DROP FUNCTION IF EXISTS uuidv7();
