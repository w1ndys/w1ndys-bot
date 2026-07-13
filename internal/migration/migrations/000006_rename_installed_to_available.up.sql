-- 📌 影响范围：将插件及功能的 installed 可用性字段重命名为 available，保留现有布尔值和索引。
ALTER TABLE plugin_definitions RENAME COLUMN installed TO available;
ALTER TABLE plugin_features RENAME COLUMN installed TO available;
