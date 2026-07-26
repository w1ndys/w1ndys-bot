-- 创建 V2 插件小型配置表；配置值随目录同步以空对象初始化，默认值由 Schema 在读取时补齐。
CREATE TABLE plugin_runtime_configs (
    plugin_key VARCHAR(64) PRIMARY KEY REFERENCES plugin_states(plugin_key) ON DELETE CASCADE,
    config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_plugin_runtime_configs_version CHECK (version > 0),
    CONSTRAINT chk_plugin_runtime_configs_object CHECK (jsonb_typeof(config_json) = 'object')
);
