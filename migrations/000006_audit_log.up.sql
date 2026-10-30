-- Append-only record of state-changing backoffice and system actions
-- (ADR 0013). Entries are written in the same transaction as the change.
CREATE TABLE audit_log (
    id             UUID        PRIMARY KEY,
    actor_id       UUID,                            -- NULL for system actions
    actor_role     user_role,
    action         TEXT        NOT NULL,            -- e.g. user.block
    entity_type    TEXT        NOT NULL,
    entity_id      TEXT        NOT NULL,
    before         JSONB,
    after          JSONB,
    reason         TEXT        NOT NULL DEFAULT '',
    correlation_id TEXT        NOT NULL DEFAULT '',
    ip_address     TEXT        NOT NULL DEFAULT '',
    user_agent     TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_entity_idx ON audit_log (entity_type, entity_id, created_at DESC);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_id, created_at DESC);

CREATE FUNCTION audit_log_is_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only';
END;
$$;

CREATE TRIGGER audit_log_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON audit_log
    FOR EACH STATEMENT EXECUTE FUNCTION audit_log_is_append_only();

-- Lists the sessions a user started recently, to deny their access tokens
-- when every session of the user is revoked.
CREATE INDEX refresh_tokens_user_created_idx ON refresh_tokens (user_id, created_at);
