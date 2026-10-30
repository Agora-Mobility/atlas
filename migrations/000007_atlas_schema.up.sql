-- Moves the application objects out of public into the atlas schema
-- (ADR 0014). The migration runner creates the schema and sets
-- search_path = atlas, public, so databases created after this change
-- already have everything in atlas and this migration does nothing there.
-- Indexes, constraints, sequences and triggers follow their tables.
DO $$
DECLARE
    name text;
BEGIN
    FOREACH name IN ARRAY ARRAY['clients', 'users', 'refresh_tokens', 'outbox', 'audit_log'] LOOP
        IF to_regclass('public.' || name) IS NOT NULL THEN
            EXECUTE format('ALTER TABLE public.%I SET SCHEMA atlas', name);
        END IF;
    END LOOP;

    FOREACH name IN ARRAY ARRAY['client_type', 'user_role', 'user_status'] LOOP
        IF to_regtype('public.' || name) IS NOT NULL THEN
            EXECUTE format('ALTER TYPE public.%I SET SCHEMA atlas', name);
        END IF;
    END LOOP;

    IF to_regprocedure('public.audit_log_is_append_only()') IS NOT NULL THEN
        ALTER FUNCTION public.audit_log_is_append_only() SET SCHEMA atlas;
    END IF;

    -- Nobody but the owner may create objects in public, so nothing placed
    -- there can shadow ours through the search_path (CVE-2018-1058). This is
    -- the default since PostgreSQL 15; a role that does not own public
    -- cannot change it and leaves it to the DBA.
    BEGIN
        REVOKE CREATE ON SCHEMA public FROM PUBLIC;
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'not the owner of schema public: CREATE was not revoked';
    END;
END $$;
