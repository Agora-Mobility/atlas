ALTER TABLE workshops
    ADD COLUMN capacity     INTEGER NOT NULL DEFAULT 1 CHECK (capacity BETWEEN 1 AND 500),
    ADD COLUMN about        TEXT    NOT NULL DEFAULT '',
    ADD COLUMN warranty     TEXT    NOT NULL DEFAULT '',
    ADD COLUMN rating_sum   BIGINT  NOT NULL DEFAULT 0 CHECK (rating_sum >= 0),
    ADD COLUMN rating_count INTEGER NOT NULL DEFAULT 0 CHECK (rating_count >= 0);

ALTER TABLE workshop_services ADD COLUMN category_id UUID REFERENCES catalog_items (id);
CREATE INDEX workshop_services_category_idx ON workshop_services (category_id) WHERE category_id IS NOT NULL;

CREATE TABLE workshop_hours (
    workshop_id  UUID     NOT NULL REFERENCES workshops (id) ON DELETE CASCADE,
    weekday      SMALLINT NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    open_minute  INTEGER  NOT NULL CHECK (open_minute BETWEEN 0 AND 1439),
    close_minute INTEGER  NOT NULL CHECK (close_minute BETWEEN 1 AND 1440),
    PRIMARY KEY (workshop_id, weekday),
    CHECK (close_minute > open_minute)
);

CREATE TABLE workshop_closures (
    workshop_id UUID NOT NULL REFERENCES workshops (id) ON DELETE CASCADE,
    closed_on   DATE NOT NULL,
    reason      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (workshop_id, closed_on)
);

CREATE TABLE workshop_mechanics (
    id          UUID        PRIMARY KEY,
    workshop_id UUID        NOT NULL REFERENCES workshops (id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    is_active   BOOLEAN     NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX workshop_mechanics_workshop_idx ON workshop_mechanics (workshop_id) WHERE is_active;

CREATE TABLE workshop_favorites (
    customer_id UUID        NOT NULL REFERENCES users (id),
    workshop_id UUID        NOT NULL REFERENCES workshops (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (customer_id, workshop_id)
);

ALTER TABLE vouchers
    ADD COLUMN workshop_id         UUID REFERENCES workshops (id),
    ADD COLUMN workshop_service_id UUID REFERENCES workshop_services (id),
    ADD COLUMN auto_apply          BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT vouchers_auto_apply_workshop CHECK (NOT auto_apply OR workshop_id IS NOT NULL);

CREATE INDEX vouchers_workshop_idx ON vouchers (workshop_id) WHERE workshop_id IS NOT NULL AND is_active;

ALTER TABLE service_requests
    ADD COLUMN workshop_id     UUID REFERENCES workshops (id),
    ADD COLUMN prepay          BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN extra_due_cents BIGINT  NOT NULL DEFAULT 0 CHECK (extra_due_cents >= 0);

CREATE TABLE bookings (
    id                UUID        PRIMARY KEY,
    request_id        UUID        NOT NULL UNIQUE REFERENCES service_requests (id),
    workshop_id       UUID        NOT NULL REFERENCES workshops (id),
    customer_id       UUID        NOT NULL REFERENCES users (id),
    vehicle_id        UUID        NOT NULL REFERENCES customer_vehicles (id),
    mechanic_id       UUID        REFERENCES workshop_mechanics (id),
    status            TEXT        NOT NULL CHECK (status IN ('awaiting_payment', 'confirmed', 'vehicle_received', 'in_service', 'ready', 'completed', 'cancelled')),
    at_home           BOOLEAN     NOT NULL DEFAULT false,
    home_fee_cents    BIGINT      NOT NULL DEFAULT 0 CHECK (home_fee_cents >= 0),
    services_cents    BIGINT      NOT NULL CHECK (services_cents >= 0),
    starts_at         TIMESTAMPTZ NOT NULL,
    ends_at           TIMESTAMPTZ NOT NULL,
    expected_ready_at TIMESTAMPTZ,
    description       TEXT        NOT NULL DEFAULT '',
    mileage_km        INTEGER     CHECK (mileage_km BETWEEN 0 AND 5000000),
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    CHECK (ends_at > starts_at)
);

CREATE INDEX bookings_workshop_day_idx ON bookings (workshop_id, starts_at) WHERE status <> 'cancelled';
CREATE INDEX bookings_customer_idx ON bookings (customer_id, starts_at DESC);

CREATE TABLE booking_items (
    id               UUID    PRIMARY KEY,
    booking_id       UUID    NOT NULL REFERENCES bookings (id) ON DELETE CASCADE,
    service_id       UUID    NOT NULL REFERENCES workshop_services (id),
    name             TEXT    NOT NULL,
    price_cents      BIGINT  NOT NULL CHECK (price_cents >= 0),
    duration_minutes INTEGER NOT NULL CHECK (duration_minutes >= 0)
);

CREATE INDEX booking_items_booking_idx ON booking_items (booking_id);

CREATE TABLE booking_extras (
    id           UUID        PRIMARY KEY,
    booking_id   UUID        NOT NULL REFERENCES bookings (id) ON DELETE CASCADE,
    description  TEXT        NOT NULL,
    amount_cents BIGINT      NOT NULL CHECK (amount_cents > 0),
    status       TEXT        NOT NULL CHECK (status IN ('pending', 'approved', 'declined')),
    decided_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX booking_extras_booking_idx ON booking_extras (booking_id, created_at);

CREATE TABLE booking_events (
    id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    booking_id  UUID        NOT NULL REFERENCES bookings (id) ON DELETE CASCADE,
    from_status TEXT        NOT NULL,
    to_status   TEXT        NOT NULL,
    actor_type  TEXT        NOT NULL,
    actor_id    UUID,
    occurred_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX booking_events_booking_idx ON booking_events (booking_id, id);

ALTER TABLE ratings ADD COLUMN workshop_id UUID REFERENCES workshops (id);
CREATE INDEX ratings_workshop_idx ON ratings (workshop_id, created_at DESC) WHERE workshop_id IS NOT NULL;

ALTER TABLE payments DROP CONSTRAINT payments_kind_check;
ALTER TABLE payments ADD CONSTRAINT payments_kind_check CHECK (kind IN ('fare', 'cancellation_fee', 'tip', 'extra'));

ALTER TABLE ledger_entries DROP CONSTRAINT ledger_entries_account_type_check;
ALTER TABLE ledger_entries DROP CONSTRAINT ledger_entries_check;
ALTER TABLE ledger_entries
    ADD CONSTRAINT ledger_entries_account_type_check CHECK (account_type IN ('provider', 'wallet', 'platform', 'external', 'workshop')),
    ADD CONSTRAINT ledger_entries_check CHECK ((account_type IN ('provider', 'wallet', 'workshop')) = (account_id IS NOT NULL));

ALTER TABLE request_messages DROP CONSTRAINT request_messages_sender_type_check;
ALTER TABLE request_messages ADD CONSTRAINT request_messages_sender_type_check CHECK (sender_type IN ('customer', 'provider', 'workshop'));

ALTER TABLE dispatch_policies ADD COLUMN booking_only BOOLEAN NOT NULL DEFAULT false;
UPDATE dispatch_policies SET booking_only = true WHERE category_id = (SELECT id FROM service_categories WHERE slug = 'maintenance');

UPDATE service_categories SET code_prefix = 'MK' WHERE slug = 'maintenance';

UPDATE category_payment_methods SET is_enabled = false WHERE method = 'cash' AND category_id = (SELECT id FROM service_categories WHERE slug = 'maintenance');

INSERT INTO catalog_items (kind, code, label, display_order) VALUES
    ('workshop_category', 'review', 'Revisão', 1),
    ('workshop_category', 'oil', 'Óleo e filtros', 2),
    ('workshop_category', 'tires', 'Pneus', 3),
    ('workshop_category', 'brakes', 'Travões', 4),
    ('workshop_category', 'electric', 'Elétrica', 5),
    ('workshop_category', 'bodywork', 'Chapa e pintura', 6);

UPDATE services SET description = 'Marcação numa oficina parceira, na oficina ou ao domicílio.' WHERE slug = 'maintenance';

INSERT INTO platform_settings (key, value) VALUES ('booking_max_days', '30'), ('booking_slot_minutes', '30');
