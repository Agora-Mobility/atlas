-- name: NextTicketCode :one
SELECT ('TK-' || nextval('support_ticket_code_seq'))::text;

-- name: InsertTicket :exec
INSERT INTO support_tickets (id, code, user_id, request_id, topic_id, subject, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: GetTicket :one
SELECT * FROM support_tickets WHERE id = $1;

-- name: ListTicketsOfUser :many
SELECT * FROM support_tickets WHERE user_id = $1 ORDER BY created_at DESC LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountTicketsOfUser :one
SELECT count(*) FROM support_tickets WHERE user_id = $1;

-- name: ListTickets :many
SELECT * FROM support_tickets WHERE sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text
ORDER BY created_at DESC LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountTickets :one
SELECT count(*) FROM support_tickets WHERE sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text;

-- name: SetTicketStatus :execrows
UPDATE support_tickets SET status = $2, updated_at = now() WHERE id = $1;

-- name: InsertTicketMessage :exec
INSERT INTO support_messages (ticket_id, sender_type, sender_id, body, created_at) VALUES ($1, $2, $3, $4, $5);

-- name: ListTicketMessages :many
SELECT * FROM support_messages WHERE ticket_id = $1 ORDER BY id;
