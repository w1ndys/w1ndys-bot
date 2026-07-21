-- 📌 影响范围：创建群违禁消息监控的发言计数、白名单、违规审计、误判反馈与周期权重偏移表。
CREATE TABLE forbidden_monitor_daily_speech_counts (
    group_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    speech_date DATE NOT NULL,
    valid_count INTEGER NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, user_id, speech_date),
    CONSTRAINT chk_forbidden_monitor_speech_group_id CHECK (group_id > 0),
    CONSTRAINT chk_forbidden_monitor_speech_user_id CHECK (user_id > 0),
    CONSTRAINT chk_forbidden_monitor_speech_valid_count CHECK (valid_count >= 0),
    CONSTRAINT chk_forbidden_monitor_speech_version CHECK (version > 0)
);

CREATE INDEX idx_forbidden_monitor_speech_recent
    ON forbidden_monitor_daily_speech_counts (group_id, speech_date DESC, user_id);

CREATE TABLE forbidden_monitor_whitelist (
    group_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version BIGINT NOT NULL DEFAULT 1,
    PRIMARY KEY (group_id, user_id),
    CONSTRAINT chk_forbidden_monitor_whitelist_group_id CHECK (group_id > 0),
    CONSTRAINT chk_forbidden_monitor_whitelist_user_id CHECK (user_id > 0),
    CONSTRAINT chk_forbidden_monitor_whitelist_version CHECK (version > 0)
);

CREATE INDEX idx_forbidden_monitor_whitelist_refresh
    ON forbidden_monitor_whitelist (refreshed_at, group_id);

CREATE TABLE forbidden_monitor_violation_audits (
    id BIGSERIAL PRIMARY KEY,
    message_id BIGINT,
    msg_content TEXT NOT NULL,
    group_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending_review',
    detection_source VARCHAR(32) NOT NULL,
    risk_score SMALLINT,
    reason TEXT NOT NULL DEFAULT '',
    violations JSONB NOT NULL DEFAULT '[]'::jsonb,
    action_result JSONB NOT NULL DEFAULT '{}'::jsonb,
    message_time TIMESTAMPTZ NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_forbidden_monitor_violation_message UNIQUE (group_id, message_id),
    CONSTRAINT chk_forbidden_monitor_violation_group_id CHECK (group_id > 0),
    CONSTRAINT chk_forbidden_monitor_violation_user_id CHECK (user_id > 0),
    CONSTRAINT chk_forbidden_monitor_violation_content CHECK (length(msg_content) > 0),
    CONSTRAINT chk_forbidden_monitor_violation_status CHECK (status IN ('pending_review', 'confirmed_pending_kick', 'confirmed_kicked', 'false_positive_unban_pending', 'false_positive_unbanned')),
    CONSTRAINT chk_forbidden_monitor_violation_source CHECK (detection_source IN ('precise_rule', 'weighted_score', 'llm')),
    CONSTRAINT chk_forbidden_monitor_violation_score CHECK (risk_score IS NULL OR risk_score BETWEEN 0 AND 100),
    CONSTRAINT chk_forbidden_monitor_violation_violations CHECK (jsonb_typeof(violations) = 'array'),
    CONSTRAINT chk_forbidden_monitor_violation_action_result CHECK (jsonb_typeof(action_result) = 'object'),
    CONSTRAINT chk_forbidden_monitor_violation_version CHECK (version > 0)
);

CREATE INDEX idx_forbidden_monitor_violation_review
    ON forbidden_monitor_violation_audits (status, created_at DESC, id DESC);
CREATE INDEX idx_forbidden_monitor_violation_user_time
    ON forbidden_monitor_violation_audits (group_id, user_id, message_time DESC);

CREATE TABLE forbidden_monitor_feedback_samples (
    id BIGSERIAL PRIMARY KEY,
    violation_audit_id BIGINT NOT NULL UNIQUE REFERENCES forbidden_monitor_violation_audits(id) ON DELETE CASCADE,
    msg_content TEXT NOT NULL,
    keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
    marked_source VARCHAR(16) NOT NULL,
    marked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT chk_forbidden_monitor_feedback_content CHECK (length(msg_content) > 0),
    CONSTRAINT chk_forbidden_monitor_feedback_keywords CHECK (jsonb_typeof(keywords) = 'array'),
    CONSTRAINT chk_forbidden_monitor_feedback_source CHECK (marked_source IN ('webui', 'group_ban')),
    CONSTRAINT chk_forbidden_monitor_feedback_version CHECK (version > 0)
);

CREATE INDEX idx_forbidden_monitor_feedback_marked
    ON forbidden_monitor_feedback_samples (marked_at DESC, id DESC);

CREATE TABLE forbidden_monitor_weight_offsets (
    id BIGSERIAL PRIMARY KEY,
    keyword TEXT NOT NULL,
    weight_delta NUMERIC(8, 3) NOT NULL,
    sample_count INTEGER NOT NULL,
    effective_from DATE NOT NULL,
    effective_until DATE NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_forbidden_monitor_weight_cycle UNIQUE (keyword, effective_from),
    CONSTRAINT chk_forbidden_monitor_weight_keyword CHECK (length(btrim(keyword)) BETWEEN 1 AND 200),
    CONSTRAINT chk_forbidden_monitor_weight_delta CHECK (weight_delta < 0),
    CONSTRAINT chk_forbidden_monitor_weight_samples CHECK (sample_count > 0),
    CONSTRAINT chk_forbidden_monitor_weight_period CHECK (effective_until > effective_from),
    CONSTRAINT chk_forbidden_monitor_weight_version CHECK (version > 0)
);

CREATE INDEX idx_forbidden_monitor_weight_active
    ON forbidden_monitor_weight_offsets (effective_from, effective_until, keyword);
