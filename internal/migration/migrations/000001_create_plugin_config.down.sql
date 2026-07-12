-- 📌 影响范围：删除插件配置表及其全部数据；仅用于明确的迁移回滚。
DROP TABLE IF EXISTS plugin_config;
