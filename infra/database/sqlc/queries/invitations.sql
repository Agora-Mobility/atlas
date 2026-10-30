-- name: CreateInvitation :exec
INSERT INTO invitations (id, membership_id, token_hash, created_by, expires_at, accepted_at, revoked_at, created_at)
VALUES (@id, @membership_id, @token_hash, @created_by, @expires_at, @accepted_at, @revoked_at, @created_at);

-- name: GetInvitationByHash :one
SELECT * FROM invitations
WHERE token_hash = @token_hash;

-- name: GetInvitationByHashForUpdate :one
SELECT * FROM invitations
WHERE token_hash = @token_hash
FOR UPDATE;

-- name: RevokePendingInvitations :exec
UPDATE invitations
SET revoked_at = @revoked_at
WHERE membership_id = @membership_id AND accepted_at IS NULL AND revoked_at IS NULL;

-- name: AcceptInvitation :exec
UPDATE invitations
SET accepted_at = @accepted_at
WHERE id = @id AND accepted_at IS NULL AND revoked_at IS NULL;

-- name: GetInvitationScope :one
SELECT s.organization_id::uuid AS organization_id, s.user_id::uuid AS user_id
FROM invitation_scope(@token_hash::text) AS s;
