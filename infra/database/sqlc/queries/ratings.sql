-- name: InsertRating :execrows
INSERT INTO ratings (id, request_id, customer_id, provider_id, stars, compliment_ids, comment, created_at, workshop_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (request_id) DO NOTHING;

-- name: GetRatingOfRequest :one
SELECT * FROM ratings WHERE request_id = $1;

-- name: AddProviderRating :exec
UPDATE providers SET rating_sum = rating_sum + sqlc.arg(stars)::bigint, rating_count = rating_count + 1, updated_at = now() WHERE user_id = sqlc.arg(provider_id);

-- name: ListProviderRatings :many
SELECT r.id, r.stars, r.comment, r.compliment_ids, r.created_at, r.request_id
FROM ratings r WHERE r.provider_id = $1
ORDER BY r.created_at DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountProviderRatings :one
SELECT count(*) FROM ratings WHERE provider_id = $1;

-- name: AddWorkshopRating :exec
UPDATE workshops SET rating_sum = rating_sum + sqlc.arg(stars)::bigint, rating_count = rating_count + 1, updated_at = now() WHERE id = sqlc.arg(workshop_id);

-- name: ListWorkshopRatings :many
SELECT r.id, r.stars, r.comment, r.compliment_ids, r.created_at, r.request_id
FROM ratings r WHERE r.workshop_id = $1
ORDER BY r.created_at DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountWorkshopRatings :one
SELECT count(*) FROM ratings WHERE workshop_id = $1;
