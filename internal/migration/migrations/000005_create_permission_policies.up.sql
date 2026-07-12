-- 📌 影响范围：创建插件与功能的全局/群级角色权限策略表。
CREATE TABLE permission_policies (
    id BIGSERIAL PRIMARY KEY,
    scope_type VARCHAR(16) NOT NULL,
    scope_id VARCHAR(32) NOT NULL DEFAULT '0',
    plugin_name VARCHAR(64) NOT NULL,
    feature_key VARCHAR(64),
    subject_role VARCHAR(32) NOT NULL,
    effect VARCHAR(16) NOT NULL,
    updated_by VARCHAR(32),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_permission_scope CHECK (
        (scope_type = 'global' AND scope_id = '0')
        OR (scope_type = 'group' AND scope_id <> '0')
    ),
    CONSTRAINT chk_permission_role CHECK (
        subject_role IN ('super_admin', 'group_owner', 'group_admin', 'member')
    ),
    CONSTRAINT chk_permission_effect CHECK (effect IN ('allow', 'deny')),
    CONSTRAINT fk_permission_plugin
        FOREIGN KEY (plugin_name)
        REFERENCES plugin_definitions (plugin_name)
        ON DELETE CASCADE,
    CONSTRAINT fk_permission_feature
        FOREIGN KEY (plugin_name, feature_key)
        REFERENCES plugin_features (plugin_name, feature_key)
        ON DELETE CASCADE
);

CREATE UNIQUE INDEX uq_permission_policy
    ON permission_policies (
        scope_type,
        scope_id,
        plugin_name,
        feature_key,
        subject_role
    ) NULLS NOT DISTINCT;

CREATE INDEX idx_permission_policy_lookup
    ON permission_policies (plugin_name, feature_key, scope_type, scope_id);
