-- 📌 影响范围：新增WebUI主动投喂的违禁训练样本及候选词证据；不修改真实群违规审计。
CREATE TABLE forbidden_monitor_training_samples (
    id BIGSERIAL PRIMARY KEY,
    content_sha256 CHAR(64) NOT NULL UNIQUE,
    msg_content TEXT NOT NULL,
    keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
    marked_by TEXT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_forbidden_monitor_training_content CHECK (length(msg_content) BETWEEN 1 AND 4000),
    CONSTRAINT chk_forbidden_monitor_training_keywords CHECK (jsonb_typeof(keywords) = 'array'),
    CONSTRAINT chk_forbidden_monitor_training_hash CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_forbidden_monitor_training_actor CHECK (length(btrim(marked_by)) > 0),
    CONSTRAINT chk_forbidden_monitor_training_version CHECK (version > 0)
);

CREATE TABLE forbidden_monitor_candidate_training_evidence (
    keyword TEXT NOT NULL REFERENCES forbidden_monitor_risk_candidates(keyword) ON DELETE CASCADE,
    training_sample_id BIGINT NOT NULL REFERENCES forbidden_monitor_training_samples(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (keyword, training_sample_id)
);

CREATE INDEX idx_forbidden_monitor_training_created
    ON forbidden_monitor_training_samples (created_at DESC, id DESC);
