ALTER TABLE providers DROP COLUMN cancelled_requests;
ALTER TABLE request_offers DROP COLUMN reject_note, DROP COLUMN reject_reason_id;
ALTER TABLE service_requests DROP COLUMN cancel_fee_cents, DROP COLUMN cancel_reason_id;
DROP TABLE cancellation_rules;
DELETE FROM rejection_reasons WHERE scope IN ('customer_cancel', 'provider_cancel', 'offer_reject');
DROP INDEX rejection_reasons_scope_code_key;
ALTER TABLE rejection_reasons DROP COLUMN category_id, DROP CONSTRAINT rejection_reasons_scope_check;
ALTER TABLE rejection_reasons
    ADD CONSTRAINT rejection_reasons_scope_check CHECK (scope IN ('document', 'provider', 'suspension')),
    ADD CONSTRAINT rejection_reasons_scope_code_key UNIQUE (scope, code);
