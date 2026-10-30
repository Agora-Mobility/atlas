DROP INDEX IF EXISTS audit_log_organization_idx;
ALTER TABLE audit_log DROP COLUMN IF EXISTS organization_id;
DROP INDEX IF EXISTS refresh_tokens_membership_idx;
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS membership_id;
UPDATE users u SET role = CASE
        WHEN EXISTS (
            SELECT 1 FROM memberships m
            JOIN membership_roles mr ON mr.membership_id = m.id
            JOIN roles r ON r.id = mr.role_id
            WHERE m.user_id = u.id AND r.code = 'super_admin')
        THEN 'admin'::user_role
        WHEN EXISTS (
            SELECT 1 FROM memberships m
            JOIN organizations o ON o.id = m.organization_id
            WHERE m.user_id = u.id AND o.type = 'platform')
        THEN 'operator'::user_role
        ELSE 'partner'::user_role
    END
WHERE role = 'staff';
DROP TABLE IF EXISTS user_mfa;
DROP TABLE IF EXISTS invitations;
DROP TABLE IF EXISTS membership_roles;
DROP TABLE IF EXISTS memberships;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS organizations;
DROP FUNCTION IF EXISTS system_role_is_immutable();
DROP FUNCTION IF EXISTS membership_role_matches_organization();
DROP FUNCTION IF EXISTS role_permission_matches_scope();
DROP TYPE IF EXISTS membership_status;
DROP TYPE IF EXISTS organization_type;
