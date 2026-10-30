-- Moves the application objects back to public. The atlas schema stays: it
-- holds the migrations table.
DO $$
DECLARE
    name text;
BEGIN
    FOREACH name IN ARRAY ARRAY['clients', 'users', 'refresh_tokens', 'outbox', 'audit_log'] LOOP
        IF to_regclass('atlas.' || name) IS NOT NULL THEN
            EXECUTE format('ALTER TABLE atlas.%I SET SCHEMA public', name);
        END IF;
    END LOOP;

    FOREACH name IN ARRAY ARRAY['client_type', 'user_role', 'user_status'] LOOP
        IF to_regtype('atlas.' || name) IS NOT NULL THEN
            EXECUTE format('ALTER TYPE atlas.%I SET SCHEMA public', name);
        END IF;
    END LOOP;

    IF to_regprocedure('atlas.audit_log_is_append_only()') IS NOT NULL THEN
        ALTER FUNCTION atlas.audit_log_is_append_only() SET SCHEMA public;
    END IF;
END $$;
