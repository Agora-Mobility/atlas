CREATE TABLE customer_vehicles (
    id          UUID        PRIMARY KEY,
    customer_id UUID        NOT NULL REFERENCES users (id),
    brand       TEXT        NOT NULL,
    model       TEXT        NOT NULL,
    plate       TEXT        NOT NULL,
    year        INTEGER     NOT NULL,
    fuel        TEXT        NOT NULL CHECK (fuel IN ('petrol', 'diesel', 'hybrid', 'electric', 'gas')),
    is_active   BOOLEAN     NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX customer_vehicles_customer_idx ON customer_vehicles (customer_id) WHERE is_active;
CREATE UNIQUE INDEX customer_vehicles_plate_key ON customer_vehicles (customer_id, plate) WHERE is_active;

CREATE TABLE media_uploads (
    id           UUID        PRIMARY KEY,
    owner_id     UUID        NOT NULL REFERENCES users (id),
    object_key   TEXT        NOT NULL UNIQUE,
    content_type TEXT        NOT NULL,
    size_bytes   BIGINT      NOT NULL CHECK (size_bytes > 0),
    request_id   UUID        REFERENCES service_requests (id),
    created_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX media_uploads_owner_idx ON media_uploads (owner_id, created_at DESC);
CREATE INDEX media_uploads_request_idx ON media_uploads (request_id) WHERE request_id IS NOT NULL;

INSERT INTO platform_settings (key, value) VALUES
    ('max_request_photos', '5'),
    ('max_booking_photos', '5'),
    ('photo_max_bytes', '8000000'),
    ('photo_content_types', 'image/jpeg,image/png,image/webp');
