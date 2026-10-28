-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (
    id, user_id, client_id, family_id, token_hash, expires_at, revoked_at,
    replaced_by_id, user_agent, ip_address, created_at
)
VALUES (
    @id, @user_id, @client_id, @family_id, @token_hash, @expires_at, @revoked_at,
    @replaced_by_id, @user_agent, @ip_address, @created_at
);

-- name: GetRefreshTokenByHashForUpdate :one
SELECT * FROM refresh_tokens
WHERE token_hash = @token_hash
FOR UPDATE;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET revoked_at     = @revoked_at,
    replaced_by_id = @replaced_by_id
WHERE id = @id AND revoked_at IS NULL;

-- name: RevokeRefreshTokenFamily :exec
UPDATE refresh_tokens
SET revoked_at = @revoked_at
WHERE family_id = @family_id AND revoked_at IS NULL;

-- name: RevokeUserRefreshTokens :exec
UPDATE refresh_tokens
SET revoked_at = @revoked_at
WHERE user_id = @user_id AND revoked_at IS NULL;
