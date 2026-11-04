-- name: InsertRequestMessage :exec
INSERT INTO request_messages (id, request_id, sender_id, sender_type, body, quick_reply_id, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListRequestMessages :many
SELECT * FROM request_messages WHERE request_id = $1 ORDER BY created_at, id LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: MarkRequestMessagesRead :exec
UPDATE request_messages SET read_at = sqlc.arg(read_at) WHERE request_id = sqlc.arg(request_id) AND sender_id <> sqlc.arg(reader_id) AND read_at IS NULL;

-- name: InsertRequestShare :exec
INSERT INTO request_shares (id, request_id, token_hash, expires_at, created_at) VALUES ($1, $2, $3, $4, $5);

-- name: GetRequestShareByHash :one
SELECT * FROM request_shares WHERE token_hash = $1;

-- name: RevokeRequestShares :exec
UPDATE request_shares SET revoked_at = $2 WHERE request_id = $1 AND revoked_at IS NULL;

-- name: InsertSOSAlert :execrows
INSERT INTO sos_alerts (id, request_id, user_id, lat, lng, note, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (request_id, user_id) WHERE status = 'open' DO NOTHING;

-- name: GetOpenSOSAlert :one
SELECT * FROM sos_alerts WHERE request_id = $1 AND user_id = $2 AND status = 'open';

-- name: ListSOSAlerts :many
SELECT * FROM sos_alerts WHERE sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text
ORDER BY created_at DESC LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountSOSAlerts :one
SELECT count(*) FROM sos_alerts WHERE sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text;

-- name: ResolveSOSAlert :execrows
UPDATE sos_alerts SET status = 'resolved', resolved_by = $2, resolved_at = $3 WHERE id = $1 AND status = 'open';

-- name: DeleteRequestTeam :exec
DELETE FROM request_team_members WHERE request_id = $1;

-- name: InsertRequestTeamMember :exec
INSERT INTO request_team_members (id, request_id, name, role, verified, created_at) VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListRequestTeam :many
SELECT * FROM request_team_members WHERE request_id = $1 ORDER BY role = 'lead' DESC, created_at, id;

-- name: LockCatalogItem :one
SELECT id FROM catalog_items WHERE id = $1 FOR UPDATE;

-- name: CountRemovalsOfDay :one
SELECT count(*) FROM service_requests WHERE vehicle_type_id = sqlc.arg(vehicle_type_id) AND removal_day = sqlc.arg(day)::date AND status NOT IN ('cancelled', 'expired');
