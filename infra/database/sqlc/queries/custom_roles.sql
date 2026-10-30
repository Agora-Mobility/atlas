-- name: CreateRole :exec
INSERT INTO roles (id, organization_id, scope, code, name, requires_mfa, is_system, created_at, updated_at)
VALUES (@id, @organization_id, @scope, @code, @name, @requires_mfa, false, @created_at, @updated_at);

-- name: UpdateRole :execrows
UPDATE roles
SET name = @name, requires_mfa = @requires_mfa, updated_at = @updated_at
WHERE id = @id AND NOT is_system;

-- name: DeleteRolePermissions :exec
DELETE FROM role_permissions
WHERE role_id = @role_id;

-- name: AddRolePermissions :exec
INSERT INTO role_permissions (role_id, permission_code)
SELECT @role_id::uuid, unnest(@permission_codes::text[]);

-- name: DeleteRole :execrows
DELETE FROM roles
WHERE id = @id AND NOT is_system;

-- name: RoleIsAssigned :one
SELECT EXISTS (SELECT 1 FROM membership_roles WHERE role_id = @role_id);

-- name: BumpMembershipsWithRole :many
UPDATE memberships
SET perm_version = perm_version + 1, updated_at = @updated_at
WHERE id IN (SELECT membership_id FROM membership_roles WHERE role_id = @role_id)
RETURNING id;
