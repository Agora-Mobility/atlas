DROP INDEX IF EXISTS refresh_tokens_user_created_idx;
DROP TABLE IF EXISTS audit_log;
DROP FUNCTION IF EXISTS audit_log_is_append_only();
