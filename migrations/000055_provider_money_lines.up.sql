-- What each provider earned and what the customers paid for their requests, one
-- line per movement, for the backoffice totals (Argus, actividade dos utilizadores).
-- earned_cents: the provider ledger entries other than settlements, plus the cash
-- the driver kept in hand (the ledger books a cash fare as fare minus commission
-- minus what was paid, so the cash has to be added back). Tips are not added: the
-- ledger already credits the whole tip to the provider.
-- gross_cents: what the customers paid for the requests of the provider, net of
-- refunds, with tips, fees and extras. Duplicated payments go to the wallet and
-- are left out.
CREATE VIEW provider_money_lines AS
SELECT e.account_id AS provider_id, e.created_at AS occurred_at, 0::bigint AS gross_cents, e.amount_cents AS earned_cents
FROM ledger_entries e
JOIN ledger_transactions t ON t.id = e.transaction_id
WHERE e.account_type = 'provider' AND t.kind <> 'settlement'
UNION ALL
SELECT r.provider_id, p.paid_at AS occurred_at, p.amount_cents - p.refunded_cents AS gross_cents,
       CASE WHEN p.method = 'cash' AND p.kind <> 'tip' THEN p.amount_cents ELSE 0 END AS earned_cents
FROM payments p
JOIN service_requests r ON r.id = p.request_id
WHERE p.status = 'paid' AND NOT p.duplicate AND r.provider_id IS NOT NULL AND r.workshop_id IS NULL;
