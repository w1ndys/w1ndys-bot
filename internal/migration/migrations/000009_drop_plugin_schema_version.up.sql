-- 📌 影响范围：移除未被运行时使用的插件 Schema 版本字段，减少插件元数据维护成本。
ALTER TABLE plugin_definitions DROP COLUMN IF EXISTS schema_version;
