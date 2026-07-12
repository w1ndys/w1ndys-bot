-- 📌 影响范围：创建插件配置与运行时开关表及启用状态索引。
CREATE TABLE plugin_config (
    plugin_name VARCHAR(64) PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    config_json JSONB NOT NULL DEFAULT '{}',
    priority INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_plugin_config_enabled ON plugin_config (enabled);
