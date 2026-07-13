-- 📌 影响范围：扩展 permission_policies，使角色与指定 QQ 用户均可作为权限主体。
DROP INDEX uq_permission_policy;
ALTER TABLE permission_policies DROP CONSTRAINT chk_permission_role;
ALTER TABLE permission_policies RENAME COLUMN subject_role TO subject_id;
ALTER TABLE permission_policies ADD COLUMN subject_type VARCHAR(16) NOT NULL DEFAULT 'role';
ALTER TABLE permission_policies ALTER COLUMN subject_type DROP DEFAULT;
ALTER TABLE permission_policies ADD CONSTRAINT chk_permission_subject_type CHECK (subject_type IN ('role', 'user'));
ALTER TABLE permission_policies ADD CONSTRAINT chk_permission_subject CHECK (
    (subject_type = 'role' AND subject_id IN ('super_admin', 'group_owner', 'group_admin', 'member'))
    OR (subject_type = 'user' AND subject_id ~ '^[1-9][0-9]*$')
);
CREATE UNIQUE INDEX uq_permission_policy ON permission_policies (
    scope_type, scope_id, plugin_name, feature_key, subject_type, subject_id
) NULLS NOT DISTINCT;
CREATE INDEX idx_permission_policy_subject
    ON permission_policies (subject_type, subject_id, plugin_name, scope_type, scope_id);
