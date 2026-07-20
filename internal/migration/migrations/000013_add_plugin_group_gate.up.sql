-- 📌 影响范围：为插件增加群默认开关，并创建逐群覆盖表及查询索引。
ALTER TABLE plugin_definitions
    ADD COLUMN group_controllable BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE plugin_config
    ADD COLUMN group_default_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN group_default_version BIGINT NOT NULL DEFAULT 1;

CREATE TABLE plugin_group_overrides (
    plugin_name VARCHAR(64) NOT NULL REFERENCES plugin_definitions(plugin_name) ON DELETE CASCADE,
    group_id BIGINT NOT NULL,
    enabled BOOLEAN NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (plugin_name, group_id),
    CONSTRAINT chk_plugin_group_override_group_id CHECK (group_id > 0)
);

CREATE INDEX idx_plugin_group_overrides_group ON plugin_group_overrides (group_id, plugin_name);
