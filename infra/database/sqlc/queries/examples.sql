-- name: CreateExample :one
INSERT INTO examples (id, name, description, location, created_at, updated_at)
VALUES (
    @id, @name, @description,
    ST_SetSRID(ST_MakePoint(@longitude::float8, @latitude::float8), 4326),
    @created_at, @updated_at
)
RETURNING id, name, description,
    ST_Y(location)::float8 AS latitude,
    ST_X(location)::float8 AS longitude,
    created_at, updated_at;

-- name: GetExampleByID :one
SELECT id, name, description,
    ST_Y(location)::float8 AS latitude,
    ST_X(location)::float8 AS longitude,
    created_at, updated_at
FROM examples
WHERE id = @id;

-- name: ListExamples :many
SELECT id, name, description,
    ST_Y(location)::float8 AS latitude,
    ST_X(location)::float8 AS longitude,
    created_at, updated_at
FROM examples
ORDER BY created_at DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountExamples :one
SELECT count(*) FROM examples;
