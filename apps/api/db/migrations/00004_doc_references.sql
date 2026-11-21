-- +goose Up
-- =====================================================================
-- 迁移 00004：文档引用与参考文献
--
-- 产品背景（PRD D-06）：正文里插入引用，文末自动生成参考文献表，
-- 支持 BibTeX 导入与多种引用格式（GB/T 7714、APA、IEEE）切换。
--
-- 模型选择：引用挂在文档上，而不是塞进正文 HTML。
-- 理由：
--   1. 引用格式切换是「渲染时」的决定，正文不该固化成某一种格式；
--   2. 同一篇文献在一篇文档里只应有一条书目记录，多条引用共享它；
--   3. 换格式只需改渲染参数，不必改写正文。
-- =====================================================================

CREATE TABLE doc_references (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    doc_id       uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    -- 可选关联到平台内的论文；从 BibTeX 导入的外部条目没有对应论文时为 NULL
    paper_id     uuid REFERENCES papers(id) ON DELETE SET NULL,
    -- 正文中的引用键，例如 \cite{vaswani2017} 或 [@vaswani2017]
    cite_key     varchar(64) NOT NULL,
    -- 书目字段。从 BibTeX 导入时原样保留，手工添加时逐项填写。
    entry_type   varchar(32) NOT NULL DEFAULT 'article',  -- article/inproceedings/book/misc
    title        text NOT NULL,
    authors      jsonb NOT NULL DEFAULT '[]',             -- [{given, family} 或 {literal}]
    year         integer,
    container    varchar(512),                            -- 期刊名 / 会议名 / 出版社
    volume       varchar(32),
    issue        varchar(32),
    pages        varchar(64),
    doi          varchar(128),
    url          text,
    publisher    varchar(256),
    -- 原始 BibTeX 文本：保留下来以便用户回看与再次导出，避免往返丢失信息
    raw_bibtex   text,
    created_by   uuid REFERENCES users(id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    -- 同一文档内引用键唯一：正文按 key 查找，重复会让渲染歧义
    UNIQUE (doc_id, cite_key)
);

CREATE INDEX idx_doc_references_doc ON doc_references (doc_id, created_at);
CREATE INDEX idx_doc_references_paper ON doc_references (paper_id) WHERE paper_id IS NOT NULL;

-- +goose StatementBegin
CREATE TRIGGER trg_doc_references_updated BEFORE UPDATE ON doc_references
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_doc_references_updated ON doc_references;
DROP TABLE IF EXISTS doc_references;
