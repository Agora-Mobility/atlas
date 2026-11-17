-- name: CreateAuditLog :exec
INSERT INTO audit_log (
    id, actor_id, actor_role, organization_id, action, entity_type, entity_id, before, after,
    reason, correlation_id, ip_address, user_agent, created_at
)
VALUES (
    @id, @actor_id, @actor_role, @organization_id, @action, @entity_type, @entity_id, @before, @after,
    @reason, @correlation_id, @ip_address, @user_agent, @created_at
);

-- name: ListAuditLog :many
SELECT a.id, a.actor_id, a.actor_role, u.name AS actor_name, a.organization_id, a.action, a.entity_type, a.entity_id, a.before, a.after, a.reason, a.correlation_id, a.created_at
FROM audit_log a
LEFT JOIN users u ON u.id = a.actor_id
WHERE (sqlc.narg(actor_id)::uuid IS NULL OR a.actor_id = sqlc.narg(actor_id))
  AND (sqlc.arg(entity_type)::text = '' OR a.entity_type = sqlc.arg(entity_type)::text)
  AND (sqlc.arg(entity_id)::text = '' OR a.entity_id = sqlc.arg(entity_id)::text)
  AND (sqlc.arg(action)::text = '' OR a.action = sqlc.arg(action)::text)
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR a.created_at >= sqlc.narg(created_from))
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR a.created_at < sqlc.narg(created_to))
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountAuditLog :one
SELECT count(*) FROM audit_log a
WHERE (sqlc.narg(actor_id)::uuid IS NULL OR a.actor_id = sqlc.narg(actor_id))
  AND (sqlc.arg(entity_type)::text = '' OR a.entity_type = sqlc.arg(entity_type)::text)
  AND (sqlc.arg(entity_id)::text = '' OR a.entity_id = sqlc.arg(entity_id)::text)
  AND (sqlc.arg(action)::text = '' OR a.action = sqlc.arg(action)::text)
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR a.created_at >= sqlc.narg(created_from))
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR a.created_at < sqlc.narg(created_to));
