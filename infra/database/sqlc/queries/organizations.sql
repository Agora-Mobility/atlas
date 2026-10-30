-- name: GetPlatformOrganization :one
SELECT * FROM organizations
WHERE type = 'platform';

-- name: GetOrganizationByID :one
SELECT * FROM organizations
WHERE id = @id;

-- name: LockOrganization :one
SELECT id FROM organizations
WHERE id = @id
FOR UPDATE;

-- name: CreateOrganization :exec
INSERT INTO organizations (id, type, name, nif, created_at, updated_at)
VALUES (@id, @type, @name, @nif, @created_at, @updated_at);

-- name: ListPartnerOrganizations :many
SELECT * FROM organizations
WHERE type = 'partner'
  AND (sqlc.narg(search)::text IS NULL OR name ILIKE sqlc.narg(search)::text OR nif ILIKE sqlc.narg(search)::text)
ORDER BY name
LIMIT @page_limit OFFSET @page_offset;

-- name: CountPartnerOrganizations :one
SELECT count(*) FROM organizations
WHERE type = 'partner'
  AND (sqlc.narg(search)::text IS NULL OR name ILIKE sqlc.narg(search)::text OR nif ILIKE sqlc.narg(search)::text);

-- name: UpdatePartnerOrganization :execrows
UPDATE organizations
SET name = @name, nif = @nif, updated_at = @updated_at
WHERE id = @id AND type = 'partner';
