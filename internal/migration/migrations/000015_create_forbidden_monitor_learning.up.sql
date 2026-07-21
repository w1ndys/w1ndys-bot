-- 📌 影响范围：新增违禁监控正向候选词证据与跨重启LLM每日请求计数；不修改既有审计数据。
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

CREATE INDEX idx_forbidden_monitor_candidate_weight
    ON forbidden_monitor_risk_candidates (learned_weight DESC, confirmed_count DESC, keyword);

CREATE TABLE forbidden_monitor_llm_usage_daily (
    usage_date DATE PRIMARY KEY,
    request_count INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT chk_forbidden_monitor_llm_usage_count CHECK (request_count >= 0),
    CONSTRAINT chk_forbidden_monitor_llm_usage_version CHECK (version > 0)
);
