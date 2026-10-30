-- name: ListPermissionsByScope :many
SELECT code, description, scopes::text[] AS scopes FROM permissions
WHERE @scope::organization_type = ANY (scopes)
ORDER BY code;

-- name: ListRolesForOrganization :many
SELECT * FROM roles
WHERE scope = @scope AND (organization_id IS NULL OR organization_id = @organization_id::uuid)
ORDER BY is_system DESC, name;

-- name: GetRolesByIDs :many
SELECT * FROM roles
WHERE id = ANY (@ids::uuid[]);

-- name: GetSystemRoleByCode :one
SELECT * FROM roles
WHERE organization_id IS NULL AND scope = @scope AND code = @code;

-- name: ListRolePermissions :many
SELECT role_id, permission_code FROM role_permissions
WHERE role_id = ANY (@role_ids::uuid[])
ORDER BY permission_code;
