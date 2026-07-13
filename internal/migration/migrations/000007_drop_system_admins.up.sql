-- 📌 影响范围：删除已由 SUPER_ADMIN_QQ 替代的 system_admins 表，并清理动态 WebUI 标题设置。
DROP TABLE IF EXISTS system_admins;
DELETE FROM system_settings WHERE setting_key = 'webui_title';
