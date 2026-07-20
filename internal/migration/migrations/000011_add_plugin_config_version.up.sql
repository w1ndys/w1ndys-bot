-- 📌 影响范围：为 plugin_config 增加声明式配置乐观锁版本号。
ALTER TABLE plugin_config
    ADD COLUMN config_version BIGINT NOT NULL DEFAULT 1;
