-- 创建当前应用所需的完整初始数据库结构；所有持久化时间均使用带时区类型。
CREATE TABLE system_settings (
    setting_key VARCHAR(64) PRIMARY KEY,
    setting_value JSONB NOT NULL,
    description VARCHAR(255) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE admin_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    actor_id VARCHAR(64) NOT NULL,
    actor_role VARCHAR(32) NOT NULL,
    channel VARCHAR(16) NOT NULL,
    action VARCHAR(64) NOT NULL,
    target_type VARCHAR(64) NOT NULL,
    target_id VARCHAR(255) NOT NULL,
    before_json JSONB,
    after_json JSONB,
    success BOOLEAN NOT NULL,
    error_message TEXT,
    request_id VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_audit_channel CHECK (channel IN ('webui', 'qq', 'system'))
);

CREATE INDEX idx_admin_audit_actor_time ON admin_audit_logs (actor_id, created_at DESC);
CREATE INDEX idx_admin_audit_target_time ON admin_audit_logs (target_type, target_id, created_at DESC);

CREATE TABLE plugin_states (
    plugin_key VARCHAR(64) PRIMARY KEY,
    desired_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_plugin_states_key CHECK (plugin_key ~ '^[a-z][a-z0-9_]*$'),
    CONSTRAINT chk_plugin_states_version CHECK (version > 0)
);

CREATE TABLE plugin_group_states (
    plugin_key VARCHAR(64) NOT NULL REFERENCES plugin_states(plugin_key) ON DELETE CASCADE,
    group_id BIGINT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (plugin_key, group_id),
    CONSTRAINT chk_plugin_group_states_group_id CHECK (group_id > 0),
    CONSTRAINT chk_plugin_group_states_version CHECK (version > 0)
);

CREATE INDEX idx_plugin_group_states_group ON plugin_group_states (group_id, plugin_key);

CREATE TABLE plugin_runtime_configs (
    plugin_key VARCHAR(64) PRIMARY KEY REFERENCES plugin_states(plugin_key) ON DELETE CASCADE,
    config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_plugin_runtime_configs_version CHECK (version > 0),
    CONSTRAINT chk_plugin_runtime_configs_object CHECK (jsonb_typeof(config_json) = 'object')
);

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

CREATE TABLE forbidden_monitor_risk_candidates (
    keyword TEXT PRIMARY KEY,
    confirmed_count INTEGER NOT NULL DEFAULT 0,
    learned_weight NUMERIC(8, 3) NOT NULL DEFAULT 0,
    first_confirmed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_confirmed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT chk_forbidden_monitor_candidate_keyword CHECK (length(btrim(keyword)) BETWEEN 1 AND 200),
    CONSTRAINT chk_forbidden_monitor_candidate_count CHECK (confirmed_count >= 0),
    CONSTRAINT chk_forbidden_monitor_candidate_weight CHECK (learned_weight IN (0, 10, 20, 30)),
    CONSTRAINT chk_forbidden_monitor_candidate_version CHECK (version > 0)
);

CREATE INDEX idx_forbidden_monitor_candidate_weight
    ON forbidden_monitor_risk_candidates (learned_weight DESC, confirmed_count DESC, keyword);

CREATE TABLE forbidden_monitor_candidate_evidence (
    keyword TEXT NOT NULL REFERENCES forbidden_monitor_risk_candidates(keyword) ON DELETE CASCADE,
    violation_audit_id BIGINT NOT NULL REFERENCES forbidden_monitor_violation_audits(id) ON DELETE CASCADE,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    marked_source VARCHAR(24) NOT NULL,
    marked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version BIGINT NOT NULL DEFAULT 1,
    PRIMARY KEY (keyword, violation_audit_id),
    CONSTRAINT chk_forbidden_monitor_evidence_source CHECK (marked_source IN ('webui', 'group_decrease')),
    CONSTRAINT chk_forbidden_monitor_evidence_version CHECK (version > 0)
);

CREATE TABLE forbidden_monitor_llm_usage_daily (
    usage_date DATE PRIMARY KEY,
    request_count INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT chk_forbidden_monitor_llm_usage_count CHECK (request_count >= 0),
    CONSTRAINT chk_forbidden_monitor_llm_usage_version CHECK (version > 0)
);

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

CREATE INDEX idx_forbidden_monitor_training_created
    ON forbidden_monitor_training_samples (created_at DESC, id DESC);

CREATE TABLE forbidden_monitor_candidate_training_evidence (
    keyword TEXT NOT NULL REFERENCES forbidden_monitor_risk_candidates(keyword) ON DELETE CASCADE,
    training_sample_id BIGINT NOT NULL REFERENCES forbidden_monitor_training_samples(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (keyword, training_sample_id)
);

CREATE TABLE forbidden_monitor_terms (
    id BIGSERIAL PRIMARY KEY,
    kind VARCHAR(8) NOT NULL,
    text TEXT NOT NULL,
    weight DOUBLE PRECISION NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_forbidden_terms_kind CHECK (kind IN ('hard', 'risk', 'safe')),
    CONSTRAINT chk_forbidden_terms_text CHECK (length(text) BETWEEN 1 AND 100 AND length(btrim(text)) >= 1),
    CONSTRAINT chk_forbidden_terms_weight CHECK (weight >= 0 AND weight <= 1000),
    CONSTRAINT chk_forbidden_terms_version CHECK (version > 0),
    CONSTRAINT uq_forbidden_terms_kind_text UNIQUE (kind, text)
);

CREATE INDEX idx_forbidden_terms_kind ON forbidden_monitor_terms (kind, id);

CREATE TABLE forbidden_monitor_combinations (
    id BIGSERIAL PRIMARY KEY,
    terms TEXT[] NOT NULL,
    bonus DOUBLE PRECISION NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_forbidden_combinations_terms CHECK (array_length(terms, 1) BETWEEN 2 AND 8),
    CONSTRAINT chk_forbidden_combinations_bonus CHECK (bonus >= 0 AND bonus <= 1000),
    CONSTRAINT chk_forbidden_combinations_version CHECK (version > 0)
);
