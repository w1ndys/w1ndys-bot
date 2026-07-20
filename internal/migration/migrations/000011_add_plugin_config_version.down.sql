-- 📌 影响范围：回滚声明式插件配置乐观锁版本号；现有配置值保持不变。
ALTER TABLE plugin_config
    DROP COLUMN IF EXISTS config_version;
