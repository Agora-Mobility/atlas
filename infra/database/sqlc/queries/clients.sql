-- name: GetActiveClientByClientID :one
SELECT * FROM clients
WHERE client_id = @client_id AND is_active;
