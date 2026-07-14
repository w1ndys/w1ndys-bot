-- 📌 影响范围：回滚插件 Schema 版本字段移除操作，并为历史记录恢复默认版本 1。
ALTER TABLE plugin_definitions
    ADD COLUMN schema_version INTEGER NOT NULL DEFAULT 1;
