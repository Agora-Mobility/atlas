ALTER TABLE dispatch_policies
    ADD COLUMN max_stops SMALLINT NOT NULL DEFAULT 3 CHECK (max_stops BETWEEN 0 AND 20),
    ADD COLUMN schedule_slot_minutes SMALLINT NOT NULL DEFAULT 15 CHECK (schedule_slot_minutes BETWEEN 1 AND 60);

ALTER TABLE service_tariffs
    ADD COLUMN stop_fee_cents BIGINT NOT NULL DEFAULT 0 CHECK (stop_fee_cents >= 0);

ALTER TABLE fare_estimates
    ADD COLUMN scheduled_for TIMESTAMPTZ,
    ADD COLUMN used_at TIMESTAMPTZ;

ALTER TABLE service_categories
    ADD COLUMN passenger_sms TEXT NOT NULL DEFAULT 'AGORA: pediram um serviço para ti. Prestador: {provider}. {plate} {link}';

UPDATE service_categories SET passenger_sms = 'AGORA: pediram um táxi para ti. Motorista: {provider}, matrícula {plate}. Segue a viagem: {link}' WHERE slug = 'taxi';
UPDATE service_categories SET passenger_sms = 'AGORA: pediram um reboque para ti. Motorista: {provider}, matrícula {plate}. Segue o reboque: {link}' WHERE slug = 'towing';
UPDATE service_categories SET passenger_sms = 'AGORA: pediram uma mudança para ti. Responsável: {provider}, matrícula {plate}. Segue a mudança: {link}' WHERE slug = 'removals';
UPDATE service_categories SET passenger_sms = 'AGORA: pediram assistência para ti. Técnico: {provider}. {plate} {link}' WHERE slug = 'maintenance';

CREATE INDEX service_requests_customer_active_idx ON service_requests (customer_id, service_id)
    WHERE status NOT IN ('completed', 'cancelled', 'expired');
