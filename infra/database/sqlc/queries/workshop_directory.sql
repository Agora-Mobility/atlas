-- name: NearestWorkshop :one
SELECT w.id, w.name, a.lat, a.lng, a.line,
       ST_Distance(a.geom, ST_SetSRID(ST_MakePoint(sqlc.arg(lng)::float8, sqlc.arg(lat)::float8), 4326)::geography)::float8 AS distance_m
FROM workshops w JOIN addresses a ON a.id = w.address_id
WHERE w.is_active
ORDER BY a.geom <-> ST_SetSRID(ST_MakePoint(sqlc.arg(lng)::float8, sqlc.arg(lat)::float8), 4326)::geography
LIMIT 1;
