-- 📌 影响范围：删除插件逐群覆盖表及群门禁元数据。
DROP TABLE IF EXISTS plugin_group_overrides;

ALTER TABLE plugin_config
    DROP COLUMN IF EXISTS group_default_enabled,
    DROP COLUMN IF EXISTS group_default_version;

ALTER TABLE plugin_definitions
    DROP COLUMN IF EXISTS group_controllable;
