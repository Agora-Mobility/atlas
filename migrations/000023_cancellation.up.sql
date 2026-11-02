ALTER TABLE rejection_reasons DROP CONSTRAINT rejection_reasons_scope_check;
ALTER TABLE rejection_reasons DROP CONSTRAINT rejection_reasons_scope_code_key;
ALTER TABLE rejection_reasons
    ADD CONSTRAINT rejection_reasons_scope_check CHECK (scope IN ('document', 'provider', 'suspension', 'customer_cancel', 'provider_cancel', 'offer_reject')),
    ADD COLUMN category_id UUID REFERENCES service_categories (id);
CREATE UNIQUE INDEX rejection_reasons_scope_code_key ON rejection_reasons (scope, code, COALESCE(category_id, '00000000-0000-0000-0000-000000000000'::uuid));

INSERT INTO rejection_reasons (scope, code, label, requires_note, display_order, category_id)
SELECT r.scope, r.code, r.label, r.note, r.position, c.id
FROM service_categories c
JOIN (VALUES
    ('taxi', 'customer_cancel', 'driver_late', 'O motorista está a demorar', false, 1),
    ('taxi', 'customer_cancel', 'changed_plans', 'Mudei de planos', false, 2),
    ('taxi', 'customer_cancel', 'wrong_address', 'Enganei-me na morada', false, 3),
    ('taxi', 'customer_cancel', 'found_other', 'Encontrei outro transporte', false, 4),
    ('taxi', 'customer_cancel', 'other', 'Outro motivo', true, 99),
    ('taxi', 'provider_cancel', 'customer_no_show', 'O cliente não apareceu', false, 1),
    ('taxi', 'provider_cancel', 'vehicle_problem', 'Problema com a viatura', false, 2),
    ('taxi', 'provider_cancel', 'unsafe', 'Local inseguro', false, 3),
    ('taxi', 'provider_cancel', 'other', 'Outro motivo', true, 99),
    ('taxi', 'offer_reject', 'too_far', 'Muito longe', false, 1),
    ('taxi', 'offer_reject', 'low_fare', 'Valor baixo', false, 2),
    ('taxi', 'offer_reject', 'unsafe_area', 'Zona insegura', false, 3),
    ('taxi', 'offer_reject', 'other', 'Outro motivo', true, 99),
    ('towing', 'customer_cancel', 'solved', 'Já resolvi o problema', false, 1),
    ('towing', 'customer_cancel', 'too_long', 'Está a demorar muito', false, 2),
    ('towing', 'customer_cancel', 'found_other', 'Encontrei outro reboque', false, 3),
    ('towing', 'customer_cancel', 'other', 'Outro motivo', true, 99),
    ('towing', 'provider_cancel', 'vehicle_not_found', 'Não encontrei a viatura', false, 1),
    ('towing', 'provider_cancel', 'cannot_tow', 'A viatura não pode ser rebocada', false, 2),
    ('towing', 'provider_cancel', 'other', 'Outro motivo', true, 99),
    ('towing', 'offer_reject', 'too_far', 'Muito longe', false, 1),
    ('towing', 'offer_reject', 'not_compatible', 'A viatura não é compatível', false, 2),
    ('towing', 'offer_reject', 'other', 'Outro motivo', true, 99),
    ('removals', 'customer_cancel', 'changed_date', 'Mudei de data', false, 1),
    ('removals', 'customer_cancel', 'found_other', 'Encontrei outra empresa', false, 2),
    ('removals', 'customer_cancel', 'other', 'Outro motivo', true, 99),
    ('removals', 'provider_cancel', 'no_team', 'Sem equipa disponível', false, 1),
    ('removals', 'provider_cancel', 'other', 'Outro motivo', true, 99),
    ('removals', 'offer_reject', 'no_capacity', 'Sem capacidade nesse dia', false, 1),
    ('removals', 'offer_reject', 'other', 'Outro motivo', true, 99),
    ('maintenance', 'customer_cancel', 'changed_date', 'Mudei de data', false, 1),
    ('maintenance', 'customer_cancel', 'found_other', 'Encontrei outra oficina', false, 2),
    ('maintenance', 'customer_cancel', 'other', 'Outro motivo', true, 99),
    ('maintenance', 'provider_cancel', 'no_capacity', 'Sem capacidade', false, 1),
    ('maintenance', 'provider_cancel', 'other', 'Outro motivo', true, 99)
) AS r (category_slug, scope, code, label, note, position) ON r.category_slug = c.slug;

CREATE TABLE cancellation_rules (
    service_id                UUID        PRIMARY KEY REFERENCES services (id),
    fee_cents                 BIGINT      NOT NULL DEFAULT 0 CHECK (fee_cents >= 0),
    free_after_assign_seconds INTEGER     NOT NULL DEFAULT 120 CHECK (free_after_assign_seconds BETWEEN 0 AND 604800),
    scheduled_free_seconds    INTEGER     NOT NULL DEFAULT 1800 CHECK (scheduled_free_seconds BETWEEN 0 AND 2592000),
    charge_without_provider   BOOLEAN     NOT NULL DEFAULT false,
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO cancellation_rules (service_id, fee_cents, free_after_assign_seconds, scheduled_free_seconds, charge_without_provider)
SELECT s.id, r.fee, r.free_after, r.scheduled_free, r.without_provider
FROM services s
JOIN (VALUES
    ('taxi', 50000, 120, 1800, false),
    ('taxi-comfort', 75000, 120, 1800, false),
    ('taxi-xl', 100000, 120, 1800, false),
    ('towing', 200000, 120, 1800, false),
    ('removals', 1500000, 0, 86400, true),
    ('maintenance', 0, 0, 0, false)
) AS r (slug, fee, free_after, scheduled_free, without_provider) ON r.slug = s.slug;

ALTER TABLE service_requests
    ADD COLUMN cancel_reason_id UUID REFERENCES rejection_reasons (id),
    ADD COLUMN cancel_fee_cents BIGINT NOT NULL DEFAULT 0 CHECK (cancel_fee_cents >= 0);

CREATE INDEX service_requests_cancel_reason_idx ON service_requests (cancel_reason_id) WHERE cancel_reason_id IS NOT NULL;
CREATE INDEX service_requests_cancel_debt_idx ON service_requests (customer_id) WHERE status = 'cancelled' AND cancel_fee_cents > 0 AND payment_status IN ('unpaid', 'pending');

ALTER TABLE request_offers
    ADD COLUMN reject_reason_id UUID REFERENCES rejection_reasons (id),
    ADD COLUMN reject_note TEXT NOT NULL DEFAULT '';

CREATE INDEX request_offers_reject_reason_idx ON request_offers (reject_reason_id) WHERE reject_reason_id IS NOT NULL;

ALTER TABLE providers ADD COLUMN cancelled_requests INTEGER NOT NULL DEFAULT 0 CHECK (cancelled_requests >= 0);
