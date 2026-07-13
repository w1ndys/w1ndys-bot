-- 📌 影响范围：回滚 available 字段命名为 installed，保留现有布尔值和索引。
ALTER TABLE plugin_features RENAME COLUMN available TO installed;
ALTER TABLE plugin_definitions RENAME COLUMN available TO installed;
