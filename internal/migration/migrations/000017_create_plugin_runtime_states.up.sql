-- 创建 V2 插件全局意图和逐群开关表；所有新记录默认关闭。
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

CREATE INDEX idx_plugin_group_states_group
    ON plugin_group_states (group_id, plugin_key);
