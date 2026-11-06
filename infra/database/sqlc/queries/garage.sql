-- name: InsertCustomerVehicle :exec
INSERT INTO customer_vehicles (id, customer_id, brand, model, plate, year, fuel, is_active, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, true, $8, $8);

-- name: UpdateCustomerVehicle :execrows
UPDATE customer_vehicles SET brand = $3, model = $4, plate = $5, year = $6, fuel = $7, updated_at = $8 WHERE id = $1 AND customer_id = $2 AND is_active;

-- name: DeactivateCustomerVehicle :execrows
UPDATE customer_vehicles SET is_active = false, updated_at = $3 WHERE id = $1 AND customer_id = $2 AND is_active;

-- name: GetCustomerVehicle :one
SELECT * FROM customer_vehicles WHERE id = $1 AND customer_id = $2 AND is_active;

-- name: ListCustomerVehicles :many
SELECT * FROM customer_vehicles WHERE customer_id = $1 AND is_active ORDER BY created_at;

-- name: InsertMediaUpload :exec
INSERT INTO media_uploads (id, owner_id, object_key, content_type, size_bytes, created_at) VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetMediaUploads :many
SELECT * FROM media_uploads WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: GetMediaUpload :one
SELECT * FROM media_uploads WHERE id = $1;

-- name: AttachMediaToRequest :execrows
UPDATE media_uploads SET request_id = sqlc.arg(request_id) WHERE id = ANY(sqlc.arg(ids)::uuid[]) AND owner_id = sqlc.arg(owner_id) AND request_id IS NULL;

-- name: ListMediaOfRequest :many
SELECT * FROM media_uploads WHERE request_id = $1 ORDER BY created_at;
