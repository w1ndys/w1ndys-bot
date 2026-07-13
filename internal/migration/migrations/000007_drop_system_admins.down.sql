-- 📌 影响范围：回滚时恢复旧版 system_admins 表结构与默认 WebUI 标题；已删除的管理员数据无法自动恢复。
CREATE TABLE system_admins (
    user_id VARCHAR(32) PRIMARY KEY,
    nickname VARCHAR(64) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by VARCHAR(32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO system_settings (setting_key, setting_value, description)
VALUES ('webui_title', '"W1ndys Bot"'::JSONB, 'WebUI 页面标题')
ON CONFLICT (setting_key) DO NOTHING;
