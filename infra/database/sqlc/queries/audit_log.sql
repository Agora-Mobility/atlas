-- name: CreateAuditLog :exec
INSERT INTO audit_log (
    id, actor_id, actor_role, organization_id, action, entity_type, entity_id, before, after,
    reason, correlation_id, ip_address, user_agent, created_at
)
VALUES (
    @id, @actor_id, @actor_role, @organization_id, @action, @entity_type, @entity_id, @before, @after,
    @reason, @correlation_id, @ip_address, @user_agent, @created_at
);
