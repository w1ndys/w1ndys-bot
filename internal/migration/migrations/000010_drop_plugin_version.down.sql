-- 📌 影响范围：回滚插件独立版本字段移除操作，并为历史记录恢复兼容占位值。
ALTER TABLE plugin_definitions
    ADD COLUMN version VARCHAR(32) NOT NULL DEFAULT 'builtin';
