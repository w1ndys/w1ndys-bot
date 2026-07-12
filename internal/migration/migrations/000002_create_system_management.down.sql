-- 📌 影响范围：删除系统管理与审计表及其中全部数据。
DROP TABLE IF EXISTS admin_audit_logs;
DROP TABLE IF EXISTS system_admins;
DROP TABLE IF EXISTS system_settings;
