ALTER TABLE role_permissions DISABLE TRIGGER role_permissions_system_immutable;
DELETE FROM role_permissions WHERE permission_code = 'members:mfa-reset';
ALTER TABLE role_permissions ENABLE TRIGGER role_permissions_system_immutable;
DELETE FROM permissions WHERE code = 'members:mfa-reset';
DROP TABLE IF EXISTS mfa_recovery_codes;
