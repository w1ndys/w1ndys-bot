-- 📌 影响范围：移除个人开发框架不使用的插件独立版本字段。
ALTER TABLE plugin_definitions DROP COLUMN IF EXISTS version;
