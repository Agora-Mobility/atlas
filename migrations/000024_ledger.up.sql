ALTER TABLE service_requests DROP CONSTRAINT service_requests_payment_method_check;
ALTER TABLE service_requests ADD CONSTRAINT service_requests_payment_method_check CHECK (payment_method IN ('cash', 'gpo', 'reference', 'wallet'));
ALTER TABLE category_payment_methods DROP CONSTRAINT category_payment_methods_method_check;
ALTER TABLE category_payment_methods ADD CONSTRAINT category_payment_methods_method_check CHECK (method IN ('cash', 'gpo', 'reference', 'wallet'));
ALTER TABLE payments DROP CONSTRAINT payments_method_check;
ALTER TABLE payments ADD CONSTRAINT payments_method_check CHECK (method IN ('cash', 'gpo', 'reference', 'wallet'));

INSERT INTO category_payment_methods (category_id, method, is_enabled, display_order)
SELECT id, 'wallet', true, 4 FROM service_categories;

ALTER TABLE payments
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'fare' CHECK (kind IN ('fare', 'cancellation_fee', 'tip')),
    ADD COLUMN gateway_fee_cents BIGINT CHECK (gateway_fee_cents >= 0);

UPDATE payments p SET kind = 'cancellation_fee' FROM service_requests r WHERE r.id = p.request_id AND r.status = 'cancelled';

DROP INDEX payments_one_pending_key;
CREATE UNIQUE INDEX payments_one_pending_key ON payments (request_id, kind) WHERE status = 'pending';

CREATE TABLE ledger_transactions (
    id              UUID        PRIMARY KEY,
    idempotency_key TEXT        NOT NULL UNIQUE,
    kind            TEXT        NOT NULL CHECK (kind IN ('fare_payment', 'cash_payment', 'tip', 'cancellation_fee', 'refund', 'settlement', 'wallet_credit', 'gateway_fee', 'referral_reward')),
    request_id      UUID        REFERENCES service_requests (id),
    payment_id      UUID        REFERENCES payments (id),
    note            TEXT        NOT NULL DEFAULT '',
    actor_id        UUID,
    currency        CHAR(3)     NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ledger_transactions_request_idx ON ledger_transactions (request_id) WHERE request_id IS NOT NULL;

CREATE TABLE ledger_entries (
    id             BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transaction_id UUID        NOT NULL REFERENCES ledger_transactions (id),
    account_type   TEXT        NOT NULL CHECK (account_type IN ('provider', 'wallet', 'platform', 'external')),
    account_id     UUID,
    amount_cents   BIGINT      NOT NULL CHECK (amount_cents <> 0),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((account_type IN ('provider', 'wallet')) = (account_id IS NOT NULL))
);

CREATE INDEX ledger_entries_account_idx ON ledger_entries (account_type, account_id, id DESC);
CREATE INDEX ledger_entries_transaction_idx ON ledger_entries (transaction_id);
