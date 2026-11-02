CREATE TABLE vouchers (
    id                 UUID        PRIMARY KEY,
    code               TEXT        UNIQUE,
    description        TEXT        NOT NULL DEFAULT '',
    category_id        UUID        REFERENCES service_categories (id),
    service_id         UUID        REFERENCES services (id),
    discount_bps       INTEGER     NOT NULL DEFAULT 0 CHECK (discount_bps BETWEEN 0 AND 10000),
    discount_cents     BIGINT      NOT NULL DEFAULT 0 CHECK (discount_cents >= 0),
    max_discount_cents BIGINT      CHECK (max_discount_cents >= 0),
    min_fare_cents     BIGINT      NOT NULL DEFAULT 0 CHECK (min_fare_cents >= 0),
    valid_from         TIMESTAMPTZ NOT NULL,
    valid_until        TIMESTAMPTZ NOT NULL,
    max_redemptions    INTEGER     CHECK (max_redemptions > 0),
    per_customer_limit INTEGER     NOT NULL DEFAULT 1 CHECK (per_customer_limit > 0),
    is_public          BOOLEAN     NOT NULL DEFAULT false,
    is_active          BOOLEAN     NOT NULL DEFAULT true,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (valid_until > valid_from),
    CHECK ((discount_bps > 0) <> (discount_cents > 0))
);

CREATE TABLE voucher_redemptions (
    id             BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    voucher_id     UUID        NOT NULL REFERENCES vouchers (id),
    customer_id    UUID        NOT NULL REFERENCES users (id),
    request_id     UUID        UNIQUE REFERENCES service_requests (id),
    discount_cents BIGINT      NOT NULL CHECK (discount_cents >= 0),
    released_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX voucher_redemptions_voucher_idx ON voucher_redemptions (voucher_id, customer_id) WHERE released_at IS NULL;

ALTER TABLE service_requests
    ADD COLUMN voucher_id UUID REFERENCES vouchers (id),
    ADD COLUMN voucher_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN discount_cents BIGINT NOT NULL DEFAULT 0 CHECK (discount_cents >= 0);
