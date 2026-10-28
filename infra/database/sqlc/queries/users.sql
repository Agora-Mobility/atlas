-- name: CreateUser :one
INSERT INTO users (
    id, name, email, password, phone, is_verified, status, role,
    last_login_at, created_at, updated_at, deleted_at
)
VALUES (
    @id, @name, @email, @password, @phone, @is_verified, @status, @role,
    @last_login_at, @created_at, @updated_at, @deleted_at
)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = @id AND deleted_at IS NULL;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE lower(email) = lower(@email) AND deleted_at IS NULL;

-- name: GetUserByPhone :one
SELECT * FROM users
WHERE phone = @phone AND deleted_at IS NULL;

-- name: ListUsers :many
SELECT * FROM users
WHERE deleted_at IS NULL
ORDER BY created_at DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountUsers :one
SELECT count(*) FROM users
WHERE deleted_at IS NULL;

-- name: UpdateUser :one
UPDATE users
SET name        = @name,
    email       = @email,
    password    = @password,
    phone       = @phone,
    is_verified = @is_verified,
    status      = @status,
    role        = @role,
    updated_at  = @updated_at
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: UpdateUserLastLogin :exec
UPDATE users
SET last_login_at = @last_login_at
WHERE id = @id AND deleted_at IS NULL;

-- name: SoftDeleteUser :execrows
UPDATE users
SET deleted_at = @deleted_at,
    updated_at = @deleted_at
WHERE id = @id AND deleted_at IS NULL;
