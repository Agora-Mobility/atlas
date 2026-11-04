ALTER TABLE roles ADD COLUMN IF NOT EXISTS requires_mfa BOOLEAN NOT NULL DEFAULT false;

INSERT INTO permissions (code, description, scopes) VALUES
    ('members:mfa-reset', 'Repor o MFA de um membro que perdeu a app autenticadora', '{platform,partner}');

ALTER TABLE role_permissions DISABLE TRIGGER role_permissions_system_immutable;

INSERT INTO role_permissions (role_id, permission_code)
SELECT id, 'members:mfa-reset' FROM roles
WHERE organization_id IS NULL
  AND ((scope = 'platform' AND code IN ('super_admin', 'administrator')) OR (scope = 'partner' AND code = 'owner'));

ALTER TABLE role_permissions ENABLE TRIGGER role_permissions_system_immutable;

CREATE TABLE IF NOT EXISTS mfa_recovery_codes (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users (id),
    code_hash  TEXT        NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS mfa_recovery_codes_user_code_key ON mfa_recovery_codes (user_id, code_hash);

CREATE TABLE IF NOT EXISTS user_mfa (
    user_id        UUID        PRIMARY KEY REFERENCES users (id),
    secret         TEXT        NOT NULL,
    confirmed_at   TIMESTAMPTZ,
    last_used_step BIGINT      NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
