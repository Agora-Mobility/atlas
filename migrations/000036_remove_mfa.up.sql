-- MFA was removed from the authentication flow: every account signs in with
-- the phone number and the password alone. This migration drops the leftover
-- storage (secrets, recovery codes), the members:mfa-reset permission and the
-- roles.requires_mfa column.

DROP TABLE IF EXISTS user_mfa;
DROP TABLE IF EXISTS mfa_recovery_codes;

ALTER TABLE role_permissions DISABLE TRIGGER role_permissions_system_immutable;

DELETE FROM role_permissions WHERE permission_code = 'members:mfa-reset';

ALTER TABLE role_permissions ENABLE TRIGGER role_permissions_system_immutable;

DELETE FROM permissions WHERE code = 'members:mfa-reset';

ALTER TABLE roles DROP COLUMN IF EXISTS requires_mfa;
