-- 📌 影响范围：创建关键词完全匹配回复插件的业务规则表。
CREATE TABLE keyword_reply_rules (
    id BIGSERIAL PRIMARY KEY,
    keyword TEXT NOT NULL UNIQUE,
    reply_content TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_keyword_reply_keyword_length CHECK (length(keyword) BETWEEN 1 AND 200 AND length(btrim(keyword)) >= 1),
    CONSTRAINT chk_keyword_reply_content_length CHECK (length(reply_content) BETWEEN 1 AND 2000 AND length(btrim(reply_content)) >= 1)
);

CREATE INDEX idx_keyword_reply_rules_enabled ON keyword_reply_rules (enabled, id);
