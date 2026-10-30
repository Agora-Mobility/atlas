DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'atlas_app') THEN
        BEGIN
            CREATE ROLE atlas_app NOLOGIN NOBYPASSRLS;
        EXCEPTION WHEN insufficient_privilege THEN
            RAISE EXCEPTION 'cannot create role atlas_app: ask the DBA to run CREATE ROLE atlas_app NOLOGIN NOBYPASSRLS and GRANT atlas_app TO %', current_user;
        END;
    END IF;
    IF NOT pg_has_role(current_user, 'atlas_app', 'MEMBER') THEN
        EXECUTE format('GRANT atlas_app TO %I', current_user);
    END IF;
END $$;

GRANT USAGE ON SCHEMA atlas TO atlas_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA atlas TO atlas_app;
REVOKE ALL ON atlas.schema_migrations FROM atlas_app;
REVOKE UPDATE, DELETE, TRUNCATE ON atlas.audit_log FROM atlas_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA atlas TO atlas_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA atlas GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO atlas_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA atlas GRANT USAGE, SELECT ON SEQUENCES TO atlas_app;

CREATE FUNCTION current_organization_id() RETURNS uuid
LANGUAGE sql STABLE AS $$
    SELECT NULLIF(current_setting('atlas.organization_id', true), '')::uuid
$$;

CREATE FUNCTION current_user_id() RETURNS uuid
LANGUAGE sql STABLE AS $$
    SELECT NULLIF(current_setting('atlas.user_id', true), '')::uuid
$$;

ALTER TABLE memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE memberships FORCE ROW LEVEL SECURITY;
CREATE POLICY memberships_tenant ON memberships
    USING (organization_id = current_organization_id() OR user_id = current_user_id())
    WITH CHECK (organization_id = current_organization_id());

ALTER TABLE membership_roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE membership_roles FORCE ROW LEVEL SECURITY;
CREATE POLICY membership_roles_tenant ON membership_roles
    USING (EXISTS (SELECT 1 FROM memberships m WHERE m.id = membership_id));

ALTER TABLE invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE invitations FORCE ROW LEVEL SECURITY;
CREATE POLICY invitations_tenant ON invitations
    USING (EXISTS (SELECT 1 FROM memberships m WHERE m.id = membership_id));

ALTER TABLE roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE roles FORCE ROW LEVEL SECURITY;
CREATE POLICY roles_tenant ON roles
    USING (
        organization_id IS NULL
        OR organization_id = current_organization_id()
        OR organization_id IN (SELECT m.organization_id FROM memberships m)
    )
    WITH CHECK (organization_id = current_organization_id());

ALTER TABLE role_permissions ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_permissions FORCE ROW LEVEL SECURITY;
CREATE POLICY role_permissions_tenant ON role_permissions
    USING (EXISTS (SELECT 1 FROM roles r WHERE r.id = role_id));

CREATE FUNCTION invitation_scope(p_token_hash text)
RETURNS TABLE (organization_id uuid, user_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = atlas, pg_temp AS $$
    SELECT m.organization_id, m.user_id
    FROM invitations i
    JOIN memberships m ON m.id = i.membership_id
    WHERE i.token_hash = p_token_hash
$$;

CREATE FUNCTION user_has_membership_outside(p_user_id uuid, p_organization_id uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = atlas, pg_temp AS $$
    SELECT EXISTS (
        SELECT 1 FROM memberships
        WHERE user_id = p_user_id AND organization_id <> p_organization_id AND status <> 'deactivated'
    )
$$;

REVOKE ALL ON FUNCTION invitation_scope(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION user_has_membership_outside(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION invitation_scope(text) TO atlas_app;
GRANT EXECUTE ON FUNCTION user_has_membership_outside(uuid, uuid) TO atlas_app;
