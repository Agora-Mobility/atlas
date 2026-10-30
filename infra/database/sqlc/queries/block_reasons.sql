-- name: ListActiveBlockReasons :many
SELECT id, code, label, is_active, created_at, updated_at
FROM block_reasons
WHERE is_active
ORDER BY label;

-- name: ListActiveBlockReasonsByIDs :many
SELECT id, code, label, is_active, created_at, updated_at
FROM block_reasons
WHERE is_active AND id = ANY(@ids::uuid[])
ORDER BY label;

-- name: CreateUserBlock :exec
INSERT INTO user_blocks (id, user_id, blocked_by, note, blocked_at)
VALUES (@id, @user_id, @blocked_by, @note, @blocked_at);

-- name: CreateUserBlockReasons :exec
INSERT INTO user_block_reasons (user_block_id, block_reason_id)
SELECT @user_block_id::uuid, unnest(@block_reason_ids::uuid[]);
