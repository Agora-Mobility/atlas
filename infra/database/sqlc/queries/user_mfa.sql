-- name: GetUserMFA :one
SELECT * FROM user_mfa
WHERE user_id = @user_id;

-- name: SavePendingUserMFA :exec
INSERT INTO user_mfa (user_id, secret, confirmed_at, last_used_step, created_at, updated_at)
VALUES (@user_id, @secret, NULL, 0, @updated_at, @updated_at)
ON CONFLICT (user_id) DO UPDATE
SET secret = EXCLUDED.secret, last_used_step = 0, updated_at = EXCLUDED.updated_at
WHERE user_mfa.confirmed_at IS NULL;

-- name: UseUserMFAStep :execrows
UPDATE user_mfa
SET last_used_step = @step,
    confirmed_at   = COALESCE(confirmed_at, @used_at),
    updated_at     = @used_at
WHERE user_id = @user_id AND last_used_step < @step;

-- name: DeleteUserMFA :exec
DELETE FROM user_mfa
WHERE user_id = @user_id;

-- name: DeleteRecoveryCodes :exec
DELETE FROM mfa_recovery_codes
WHERE user_id = @user_id;

-- name: CreateRecoveryCodes :exec
INSERT INTO mfa_recovery_codes (user_id, code_hash, created_at)
SELECT @user_id::uuid, unnest(@code_hashes::text[]), @created_at::timestamptz;

-- name: UseRecoveryCode :execrows
UPDATE mfa_recovery_codes
SET used_at = @used_at
WHERE user_id = @user_id AND code_hash = @code_hash AND used_at IS NULL;

-- name: CountUnusedRecoveryCodes :one
SELECT count(*) FROM mfa_recovery_codes
WHERE user_id = @user_id AND used_at IS NULL;
