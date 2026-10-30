-- name: CreateMembership :exec
INSERT INTO memberships (
    id, user_id, organization_id, status, perm_version, invited_by, activated_at, created_at, updated_at
)
VALUES (
    @id, @user_id, @organization_id, @status, @perm_version, @invited_by, @activated_at, @created_at, @updated_at
);

-- name: GetMembershipInOrganization :one
SELECT * FROM memberships
WHERE id = @id AND organization_id = @organization_id;

-- name: GetMembershipInOrganizationForUpdate :one
SELECT * FROM memberships
WHERE id = @id AND organization_id = @organization_id
FOR UPDATE;

-- name: GetMembershipByID :one
SELECT * FROM memberships
WHERE id = @id;

-- name: GetMembershipByIDForUpdate :one
SELECT * FROM memberships
WHERE id = @id
FOR UPDATE;

-- name: GetMembershipByUserAndOrganization :one
SELECT * FROM memberships
WHERE user_id = @user_id AND organization_id = @organization_id;

-- name: ListActiveMembershipsForSignIn :many
SELECT m.* FROM memberships m
JOIN organizations o ON o.id = m.organization_id
WHERE m.user_id = @user_id AND o.type = @type AND m.status = 'active'
ORDER BY m.created_at, m.id;

-- name: UpdateMembership :exec
UPDATE memberships
SET status       = @status,
    perm_version = @perm_version,
    activated_at = @activated_at,
    updated_at   = @updated_at
WHERE id = @id;

-- name: ListMembers :many
SELECT m.*, u.name AS user_name, u.email AS user_email, u.phone AS user_phone, u.last_login_at AS user_last_login_at
FROM memberships m
JOIN users u ON u.id = m.user_id
WHERE m.organization_id = @organization_id
  AND u.deleted_at IS NULL
  AND (sqlc.narg(status)::membership_status IS NULL OR m.status = sqlc.narg(status)::membership_status)
  AND (sqlc.narg(membership_id)::uuid IS NULL OR m.id = sqlc.narg(membership_id)::uuid)
  AND (
      sqlc.narg(role_id)::uuid IS NULL
      OR EXISTS (SELECT 1 FROM membership_roles mr WHERE mr.membership_id = m.id AND mr.role_id = sqlc.narg(role_id)::uuid)
  )
  AND (
      sqlc.narg(search)::text IS NULL
      OR u.name ILIKE sqlc.narg(search)::text
      OR u.email ILIKE sqlc.narg(search)::text
      OR u.phone LIKE sqlc.narg(search)::text
  )
ORDER BY m.created_at DESC, m.id
LIMIT @page_limit OFFSET @page_offset;

-- name: CountMembers :one
SELECT count(*)
FROM memberships m
JOIN users u ON u.id = m.user_id
WHERE m.organization_id = @organization_id
  AND u.deleted_at IS NULL
  AND (sqlc.narg(status)::membership_status IS NULL OR m.status = sqlc.narg(status)::membership_status)
  AND (
      sqlc.narg(role_id)::uuid IS NULL
      OR EXISTS (SELECT 1 FROM membership_roles mr WHERE mr.membership_id = m.id AND mr.role_id = sqlc.narg(role_id)::uuid)
  )
  AND (
      sqlc.narg(search)::text IS NULL
      OR u.name ILIKE sqlc.narg(search)::text
      OR u.email ILIKE sqlc.narg(search)::text
      OR u.phone LIKE sqlc.narg(search)::text
  );

-- name: ListMembershipRoles :many
SELECT membership_id, role_id FROM membership_roles
WHERE membership_id = ANY (@membership_ids::uuid[]);

-- name: DeleteMembershipRoles :exec
DELETE FROM membership_roles
WHERE membership_id = @membership_id;

-- name: AddMembershipRoles :exec
INSERT INTO membership_roles (membership_id, role_id)
SELECT @membership_id::uuid, unnest(@role_ids::uuid[]);

-- name: CountOtherActiveMembersWithRole :one
SELECT count(*) FROM memberships m
JOIN membership_roles mr ON mr.membership_id = m.id
WHERE m.organization_id = @organization_id
  AND m.status = 'active'
  AND mr.role_id = @role_id
  AND m.id <> @excluded_membership_id;

-- name: ExistsActiveMemberWithRole :one
SELECT EXISTS (
    SELECT 1 FROM memberships m
    JOIN membership_roles mr ON mr.membership_id = m.id
    WHERE m.organization_id = @organization_id AND m.status = 'active' AND mr.role_id = @role_id
);

-- name: GetMembershipAccess :one
SELECT m.id, m.user_id, m.organization_id, o.type AS organization_type, m.status, m.perm_version,
       EXISTS (
           SELECT 1 FROM membership_roles mr JOIN roles r ON r.id = mr.role_id
           WHERE mr.membership_id = m.id AND r.requires_mfa
       )::boolean AS requires_mfa
FROM memberships m
JOIN organizations o ON o.id = m.organization_id
WHERE m.id = @id;

-- name: ListMembershipPermissions :many
SELECT DISTINCT rp.permission_code
FROM membership_roles mr
JOIN role_permissions rp ON rp.role_id = mr.role_id
WHERE mr.membership_id = @membership_id
ORDER BY rp.permission_code;

-- name: UserHasMembershipOutside :one
SELECT user_has_membership_outside(@user_id::uuid, @organization_id::uuid)::boolean AS outside;
