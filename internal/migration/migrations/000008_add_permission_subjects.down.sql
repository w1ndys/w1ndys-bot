-- 📌 影响范围：回滚指定用户权限模型；用户策略会删除，角色策略恢复旧 subject_role 结构。
DELETE FROM permission_policies WHERE subject_type = 'user';
DROP INDEX idx_permission_policy_subject;
DROP INDEX uq_permission_policy;
ALTER TABLE permission_policies DROP CONSTRAINT chk_permission_subject;
ALTER TABLE permission_policies DROP CONSTRAINT chk_permission_subject_type;
ALTER TABLE permission_policies DROP COLUMN subject_type;
ALTER TABLE permission_policies RENAME COLUMN subject_id TO subject_role;
ALTER TABLE permission_policies ADD CONSTRAINT chk_permission_role
    CHECK (subject_role IN ('super_admin', 'group_owner', 'group_admin', 'member'));
CREATE UNIQUE INDEX uq_permission_policy ON permission_policies (
    scope_type, scope_id, plugin_name, feature_key, subject_role
) NULLS NOT DISTINCT;
