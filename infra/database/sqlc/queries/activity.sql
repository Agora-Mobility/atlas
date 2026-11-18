-- name: AccountActivity :many
SELECT a.user_id::uuid AS user_id,
       count(*) FILTER (WHERE a.status = 'completed')::bigint AS completed_requests,
       count(*) FILTER (WHERE a.status = 'cancelled')::bigint AS cancelled_requests,
       max(a.created_at)::timestamptz AS last_request_at
FROM (
    SELECT r.customer_id AS user_id, r.status, r.created_at FROM service_requests r WHERE r.customer_id = ANY (sqlc.arg(ids)::uuid[])
    UNION ALL
    SELECT r.provider_id AS user_id, r.status, r.created_at FROM service_requests r WHERE r.provider_id = ANY (sqlc.arg(ids)::uuid[])
) a
GROUP BY a.user_id;

-- name: ProviderCategories :many
SELECT p.user_id, c.slug, c.name
FROM providers p
JOIN services s ON s.id = COALESCE(p.service_id, p.requested_service_id)
JOIN service_categories c ON c.id = s.category_id
WHERE p.user_id = ANY (sqlc.arg(ids)::uuid[]);

-- name: ProviderRatings :many
SELECT p.user_id, p.rating_sum, p.rating_count FROM providers p WHERE p.user_id = ANY (sqlc.arg(ids)::uuid[]);

-- name: ProviderMoney :many
SELECT m.provider_id::uuid AS provider_id, COALESCE(sum(m.gross_cents), 0)::bigint AS gross_cents, COALESCE(sum(m.earned_cents), 0)::bigint AS earned_cents
FROM provider_money_lines m
WHERE m.provider_id = ANY (sqlc.arg(ids)::uuid[])
  AND (sqlc.narg(from_at)::timestamptz IS NULL OR m.occurred_at >= sqlc.narg(from_at))
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR m.occurred_at < sqlc.narg(to_at))
GROUP BY m.provider_id;

-- name: ProviderLedgerBalances :many
SELECT e.account_id::uuid AS provider_id, COALESCE(sum(e.amount_cents), 0)::bigint AS balance_cents
FROM ledger_entries e
WHERE e.account_type = 'provider' AND e.account_id = ANY (sqlc.arg(ids)::uuid[])
GROUP BY e.account_id;

-- name: ProviderDailyMoney :many
SELECT (m.occurred_at AT TIME ZONE sqlc.arg(timezone)::text)::date AS day,
       COALESCE(sum(m.gross_cents), 0)::bigint AS gross_cents, COALESCE(sum(m.earned_cents), 0)::bigint AS earned_cents
FROM provider_money_lines m
WHERE m.provider_id = sqlc.arg(provider_id)::uuid AND m.occurred_at >= sqlc.arg(from_at)::timestamptz AND m.occurred_at < sqlc.arg(to_at)::timestamptz
GROUP BY 1;

-- A request is counted on the day it ended: completed_at, or the last update for a cancelled one.
-- name: ProviderDailyRequests :many
SELECT (COALESCE(r.completed_at, r.updated_at) AT TIME ZONE sqlc.arg(timezone)::text)::date AS day,
       count(*) FILTER (WHERE r.status = 'completed')::bigint AS completed_requests,
       count(*) FILTER (WHERE r.status = 'cancelled')::bigint AS cancelled_requests
FROM service_requests r
WHERE r.provider_id = sqlc.arg(provider_id)::uuid AND r.status IN ('completed', 'cancelled')
  AND COALESCE(r.completed_at, r.updated_at) >= sqlc.arg(from_at)::timestamptz
  AND COALESCE(r.completed_at, r.updated_at) < sqlc.arg(to_at)::timestamptz
GROUP BY 1;
