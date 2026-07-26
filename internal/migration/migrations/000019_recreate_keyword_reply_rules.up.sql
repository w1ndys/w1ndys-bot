-- 关键词回复迁移到目标架构：规则改为按群隔离，旧的全局规则不再适用。
DROP TABLE IF EXISTS keyword_reply_rules;

CREATE TABLE keyword_reply_rules (
    id BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL,
    keyword TEXT NOT NULL,
    reply_content TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_keyword_reply_group_id CHECK (group_id > 0),
    CONSTRAINT chk_keyword_reply_version CHECK (version > 0),
    CONSTRAINT chk_keyword_reply_keyword_length CHECK (length(keyword) BETWEEN 1 AND 200 AND length(btrim(keyword)) >= 1),
    CONSTRAINT chk_keyword_reply_content_length CHECK (length(reply_content) BETWEEN 1 AND 2000 AND length(btrim(reply_content)) >= 1),
    CONSTRAINT uq_keyword_reply_group_keyword UNIQUE (group_id, keyword)
);

CREATE INDEX idx_keyword_reply_rules_group_enabled ON keyword_reply_rules (group_id, enabled, id);
