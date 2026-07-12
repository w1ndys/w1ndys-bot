-- 📌 影响范围：创建系统设置、最高管理员与管理审计表。
CREATE TABLE system_settings (
    setting_key VARCHAR(64) PRIMARY KEY,
    setting_value JSONB NOT NULL,
    description VARCHAR(255) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE system_admins (
    user_id VARCHAR(32) PRIMARY KEY,
    nickname VARCHAR(64) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by VARCHAR(32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
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
