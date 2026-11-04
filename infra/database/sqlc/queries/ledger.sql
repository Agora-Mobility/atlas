-- name: InsertLedgerTransaction :execrows
INSERT INTO ledger_transactions (id, idempotency_key, kind, request_id, payment_id, note, actor_id, currency, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (idempotency_key) DO NOTHING;

-- name: InsertLedgerEntry :exec
INSERT INTO ledger_entries (transaction_id, account_type, account_id, amount_cents, created_at) VALUES ($1, $2, $3, $4, $5);

-- name: LockLedgerAccount :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(account)::text, 1));

-- name: LedgerBalance :one
SELECT COALESCE(sum(amount_cents), 0)::bigint FROM ledger_entries WHERE account_type = $1 AND account_id = $2;

-- name: ListLedgerEntries :many
SELECT e.id, e.amount_cents, e.created_at, t.id AS transaction_id, t.kind, t.request_id, t.payment_id, t.note, t.currency
FROM ledger_entries e JOIN ledger_transactions t ON t.id = e.transaction_id
WHERE e.account_type = $1 AND e.account_id = $2
ORDER BY e.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountLedgerEntries :one
SELECT count(*) FROM ledger_entries WHERE account_type = $1 AND account_id = $2;

-- name: LedgerTotals :many
SELECT account_type, COALESCE(sum(amount_cents), 0)::bigint AS total_cents FROM ledger_entries GROUP BY account_type ORDER BY account_type;

-- name: UnbalancedLedgerTransactions :one
SELECT count(*) FROM (SELECT transaction_id FROM ledger_entries GROUP BY transaction_id HAVING sum(amount_cents) <> 0) AS broken;

-- name: LedgerProviderBalances :many
SELECT e.account_id, u.name, COALESCE(sum(e.amount_cents), 0)::bigint AS balance_cents
FROM ledger_entries e JOIN users u ON u.id = e.account_id
WHERE e.account_type = 'provider'
GROUP BY e.account_id, u.name
ORDER BY balance_cents
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: SetPaymentGatewayFee :execrows
UPDATE payments SET gateway_fee_cents = $2, updated_at = now() WHERE id = $1 AND gateway_fee_cents IS NULL;
