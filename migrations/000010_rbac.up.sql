CREATE TYPE organization_type AS ENUM ('platform', 'partner');
CREATE TYPE membership_status AS ENUM ('invited', 'active', 'suspended', 'deactivated');

CREATE TABLE organizations (
    id         UUID              PRIMARY KEY DEFAULT gen_random_uuid(),
    type       organization_type NOT NULL,
    name       TEXT              NOT NULL,
    created_at TIMESTAMPTZ       NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ       NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX organizations_single_platform_key ON organizations (type) WHERE type = 'platform';

CREATE TABLE permissions (
    code        TEXT                PRIMARY KEY,
    description TEXT                NOT NULL,
    scopes      organization_type[] NOT NULL CHECK (cardinality(scopes) > 0)
);

CREATE TABLE roles (
    id              UUID              PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID              REFERENCES organizations (id),
    scope           organization_type NOT NULL,
    code            TEXT              NOT NULL,
    name            TEXT              NOT NULL,
    requires_mfa    BOOLEAN           NOT NULL DEFAULT false,
    is_system       BOOLEAN           NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ       NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ       NOT NULL DEFAULT now(),
    CONSTRAINT roles_system_has_no_organization_chk CHECK (is_system = (organization_id IS NULL))
);

CREATE UNIQUE INDEX roles_system_code_key ON roles (scope, code) WHERE organization_id IS NULL;
CREATE UNIQUE INDEX roles_organization_code_key ON roles (organization_id, code) WHERE organization_id IS NOT NULL;

CREATE TABLE role_permissions (
    role_id         UUID NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission_code TEXT NOT NULL REFERENCES permissions (code),
    PRIMARY KEY (role_id, permission_code)
);

CREATE TABLE memberships (
    id              UUID              PRIMARY KEY,
    user_id         UUID              NOT NULL REFERENCES users (id),
    organization_id UUID              NOT NULL REFERENCES organizations (id),
    status          membership_status NOT NULL,
    perm_version    BIGINT            NOT NULL DEFAULT 1,
    invited_by      UUID              REFERENCES users (id),
    activated_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ       NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ       NOT NULL DEFAULT now(),
    CONSTRAINT memberships_user_organization_key UNIQUE (user_id, organization_id)
);

CREATE INDEX memberships_organization_status_idx ON memberships (organization_id, status);

CREATE TABLE membership_roles (
    membership_id UUID NOT NULL REFERENCES memberships (id) ON DELETE CASCADE,
    role_id       UUID NOT NULL REFERENCES roles (id),
    PRIMARY KEY (membership_id, role_id)
);

CREATE INDEX membership_roles_role_idx ON membership_roles (role_id);

CREATE TABLE invitations (
    id            UUID        PRIMARY KEY,
    membership_id UUID        NOT NULL REFERENCES memberships (id),
    token_hash    TEXT        NOT NULL,
    created_by    UUID        REFERENCES users (id),
    expires_at    TIMESTAMPTZ NOT NULL,
    accepted_at   TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX invitations_token_hash_key ON invitations (token_hash);
CREATE INDEX invitations_membership_idx ON invitations (membership_id) WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE TABLE user_mfa (
    user_id        UUID        PRIMARY KEY REFERENCES users (id),
    secret         TEXT        NOT NULL,
    confirmed_at   TIMESTAMPTZ,
    last_used_step BIGINT      NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE refresh_tokens ADD COLUMN membership_id UUID REFERENCES memberships (id);
CREATE INDEX refresh_tokens_membership_idx ON refresh_tokens (membership_id) WHERE revoked_at IS NULL;

ALTER TABLE audit_log ADD COLUMN organization_id UUID;
CREATE INDEX audit_log_organization_idx ON audit_log (organization_id, created_at DESC);

CREATE FUNCTION role_permission_matches_scope() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    role_scope organization_type;
    allowed    organization_type[];
BEGIN
    SELECT scope INTO role_scope FROM roles WHERE id = NEW.role_id;
    SELECT scopes INTO allowed FROM permissions WHERE code = NEW.permission_code;
    IF NOT (role_scope = ANY (allowed)) THEN
        RAISE EXCEPTION 'permission % is not allowed in scope %', NEW.permission_code, role_scope;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER role_permissions_scope_check
    BEFORE INSERT OR UPDATE ON role_permissions
    FOR EACH ROW EXECUTE FUNCTION role_permission_matches_scope();

CREATE FUNCTION membership_role_matches_organization() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    role_scope        organization_type;
    role_organization UUID;
    member_type       organization_type;
    member_org        UUID;
BEGIN
    SELECT scope, organization_id INTO role_scope, role_organization FROM roles WHERE id = NEW.role_id;
    SELECT o.type, o.id INTO member_type, member_org
    FROM memberships m JOIN organizations o ON o.id = m.organization_id
    WHERE m.id = NEW.membership_id;
    IF role_scope <> member_type OR (role_organization IS NOT NULL AND role_organization <> member_org) THEN
        RAISE EXCEPTION 'role % cannot be assigned to membership %', NEW.role_id, NEW.membership_id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER membership_roles_organization_check
    BEFORE INSERT OR UPDATE ON membership_roles
    FOR EACH ROW EXECUTE FUNCTION membership_role_matches_organization();

INSERT INTO organizations (type, name) VALUES ('platform', 'AGORA');

INSERT INTO permissions (code, description, scopes) VALUES
    ('members:read', 'Ver membros da própria organização', '{platform,partner}'),
    ('members:invite', 'Convidar membros e reenviar convites', '{platform,partner}'),
    ('members:update', 'Alterar nome e telefone de um membro', '{platform,partner}'),
    ('members:suspend', 'Suspender e reactivar membros', '{platform,partner}'),
    ('members:deactivate', 'Desactivar membros (definitivo)', '{platform,partner}'),
    ('roles:read', 'Ver papéis e o catálogo de permissões', '{platform,partner}'),
    ('roles:assign', 'Atribuir papéis a membros', '{platform,partner}'),
    ('roles:manage', 'Gerir papéis', '{platform}'),
    ('partners:read', 'Ver parceiros e os seus membros', '{platform}'),
    ('partners:manage', 'Gerir parceiros e convidar os seus membros', '{platform}'),
    ('users:read', 'Ver contas de clientes e prestadores', '{platform}'),
    ('users:block', 'Bloquear e desbloquear contas de clientes e prestadores', '{platform}'),
    ('users:reset-password', 'Redefinir a palavra-passe de clientes e prestadores', '{platform}'),
    ('providers:read', 'Ver prestadores', '{platform}'),
    ('providers:approve', 'Aprovar e rejeitar prestadores', '{platform}'),
    ('providers:suspend', 'Suspender prestadores', '{platform}'),
    ('categories:manage', 'Gerir categorias', '{platform}'),
    ('services:manage', 'Gerir serviços', '{platform}'),
    ('requests:read', 'Ver pedidos de serviço', '{platform}'),
    ('requests:manage', 'Gerir pedidos de serviço', '{platform}'),
    ('requests:cancel', 'Cancelar pedidos de serviço', '{platform}'),
    ('finance:read', 'Ver dados financeiros', '{platform}'),
    ('refunds:create', 'Criar reembolsos', '{platform}'),
    ('refunds:approve', 'Aprovar reembolsos', '{platform}'),
    ('reconciliation:run', 'Correr a reconciliação', '{platform}'),
    ('audit:read', 'Ler o registo de auditoria', '{platform}'),
    ('partner.providers:read', 'Ver os prestadores do parceiro', '{partner}'),
    ('partner.providers:manage', 'Gerir os prestadores do parceiro', '{partner}'),
    ('partner.requests:read', 'Ver os pedidos do parceiro', '{partner}'),
    ('partner.finance:read', 'Ver as finanças do parceiro', '{partner}');

INSERT INTO roles (scope, code, name, requires_mfa, is_system) VALUES
    ('platform', 'super_admin', 'Super Admin', true, true),
    ('platform', 'administrator', 'Administrador', false, true),
    ('platform', 'operations', 'Operações', false, true),
    ('platform', 'finance', 'Financeiro', true, true),
    ('platform', 'support', 'Suporte', false, true),
    ('platform', 'auditor', 'Auditor', false, true),
    ('partner', 'owner', 'Proprietário', true, true),
    ('partner', 'manager', 'Gestor', false, true),
    ('partner', 'operator', 'Operador', false, true),
    ('partner', 'finance', 'Financeiro', false, true);

INSERT INTO role_permissions (role_id, permission_code)
SELECT r.id, p.code
FROM roles r
JOIN permissions p ON r.scope = ANY (p.scopes)
WHERE r.organization_id IS NULL AND (
       (r.scope = 'platform' AND r.code = 'super_admin')
    OR (r.scope = 'platform' AND r.code = 'administrator' AND p.code NOT IN ('roles:manage', 'refunds:approve'))
    OR (r.scope = 'platform' AND r.code = 'operations' AND (
           p.code LIKE 'providers:%' OR p.code LIKE 'requests:%'
        OR p.code IN ('categories:manage', 'services:manage', 'members:read', 'users:read', 'users:block')))
    OR (r.scope = 'platform' AND r.code = 'finance' AND p.code IN ('finance:read', 'refunds:create', 'refunds:approve', 'reconciliation:run'))
    OR (r.scope = 'platform' AND r.code = 'support' AND p.code IN ('members:read', 'providers:read', 'requests:read', 'requests:cancel', 'users:read'))
    OR (r.scope = 'platform' AND r.code = 'auditor' AND (p.code LIKE '%:read' OR p.code = 'audit:read'))
    OR (r.scope = 'partner' AND r.code = 'owner')
    OR (r.scope = 'partner' AND r.code = 'manager' AND (
           p.code IN ('members:read', 'members:invite', 'members:update', 'members:suspend', 'roles:read', 'roles:assign', 'partner.requests:read')
        OR p.code LIKE 'partner.providers:%'))
    OR (r.scope = 'partner' AND r.code = 'operator' AND p.code IN ('partner.providers:read', 'partner.providers:manage', 'partner.requests:read'))
    OR (r.scope = 'partner' AND r.code = 'finance' AND p.code IN ('partner.finance:read', 'partner.requests:read'))
);

INSERT INTO memberships (id, user_id, organization_id, status, activated_at)
SELECT gen_random_uuid(), u.id, o.id,
       CASE WHEN u.status = 'active' THEN 'active'::membership_status ELSE 'suspended'::membership_status END,
       u.created_at
FROM users u
CROSS JOIN organizations o
WHERE o.type = 'platform' AND u.role IN ('admin', 'operator') AND u.deleted_at IS NULL;

INSERT INTO membership_roles (membership_id, role_id)
SELECT m.id, r.id
FROM memberships m
JOIN users u ON u.id = m.user_id
JOIN roles r ON r.scope = 'platform' AND r.organization_id IS NULL
            AND r.code = CASE u.role WHEN 'admin' THEN 'super_admin' ELSE 'operations' END;

UPDATE users SET status = 'active', updated_at = now()
WHERE role IN ('admin', 'operator') AND status <> 'active' AND deleted_at IS NULL;

UPDATE users SET role = 'staff', updated_at = now()
WHERE role IN ('admin', 'operator', 'partner');

CREATE FUNCTION system_role_is_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'roles' THEN
        IF OLD.is_system THEN
            RAISE EXCEPTION 'system roles cannot be changed';
        END IF;
        RETURN COALESCE(NEW, OLD);
    END IF;
    IF EXISTS (SELECT 1 FROM roles WHERE is_system AND id = COALESCE(NEW.role_id, OLD.role_id)) THEN
        RAISE EXCEPTION 'system roles cannot be changed';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;

CREATE TRIGGER roles_system_immutable
    BEFORE UPDATE OR DELETE ON roles
    FOR EACH ROW EXECUTE FUNCTION system_role_is_immutable();

CREATE TRIGGER role_permissions_system_immutable
    BEFORE INSERT OR UPDATE OR DELETE ON role_permissions
    FOR EACH ROW EXECUTE FUNCTION system_role_is_immutable();
