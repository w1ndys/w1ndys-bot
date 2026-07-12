-- 📌 影响范围：创建插件定义与功能元数据表，保留现有 plugin_config 运行状态表。
CREATE TABLE plugin_definitions (
    plugin_name VARCHAR(64) PRIMARY KEY,
    display_name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    version VARCHAR(32) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    priority INTEGER NOT NULL DEFAULT 0,
    schema_version INTEGER NOT NULL DEFAULT 1,
    installed BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_plugin_definitions_enabled
    ON plugin_definitions (enabled, priority DESC);

CREATE TABLE plugin_features (
    plugin_name VARCHAR(64) NOT NULL,
    feature_key VARCHAR(64) NOT NULL,
    display_name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    default_commands JSONB NOT NULL DEFAULT '[]',
    default_permissions JSONB NOT NULL DEFAULT '{}',
    metadata_json JSONB NOT NULL DEFAULT '{}',
    installed BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (plugin_name, feature_key),
    CONSTRAINT fk_plugin_features_plugin
        FOREIGN KEY (plugin_name)
        REFERENCES plugin_definitions (plugin_name)
        ON DELETE CASCADE
);
