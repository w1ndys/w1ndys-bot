-- 📌 影响范围：创建插件功能命令注册表及查询索引。
CREATE TABLE plugin_commands (
    id BIGSERIAL PRIMARY KEY,
    scope_type VARCHAR(16) NOT NULL,
    scope_id VARCHAR(32) NOT NULL DEFAULT '0',
    plugin_name VARCHAR(64) NOT NULL,
    feature_key VARCHAR(64) NOT NULL,
    command VARCHAR(128) NOT NULL,
    normalized_command VARCHAR(128) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_by VARCHAR(32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_plugin_command_scope CHECK (
        (scope_type = 'global' AND scope_id = '0')
        OR (scope_type = 'group' AND scope_id <> '0')
    ),
    CONSTRAINT uq_plugin_command_scope UNIQUE (scope_type, scope_id, normalized_command),
    CONSTRAINT fk_plugin_command_feature
        FOREIGN KEY (plugin_name, feature_key)
        REFERENCES plugin_features (plugin_name, feature_key)
        ON DELETE CASCADE
);

CREATE INDEX idx_plugin_commands_feature
    ON plugin_commands (plugin_name, feature_key, enabled);
