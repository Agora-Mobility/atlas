CREATE TABLE addresses (
    id           UUID             PRIMARY KEY,
    lat          DOUBLE PRECISION NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng          DOUBLE PRECISION NOT NULL CHECK (lng BETWEEN -180 AND 180),
    geom         geography(Point, 4326) GENERATED ALWAYS AS (ST_SetSRID(ST_MakePoint(lng, lat), 4326)::geography) STORED,
    line         TEXT             NOT NULL DEFAULT '',
    reference    TEXT             NOT NULL DEFAULT '',
    neighborhood TEXT             NOT NULL DEFAULT '',
    city         TEXT             NOT NULL DEFAULT '',
    province     TEXT             NOT NULL DEFAULT '',
    country_code TEXT             NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ      NOT NULL DEFAULT now()
);

CREATE INDEX addresses_geom_idx ON addresses USING GIST (geom);

CREATE TABLE user_addresses (
    address_id UUID        PRIMARY KEY REFERENCES addresses (id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT        NOT NULL CHECK (kind IN ('home', 'work', 'other')),
    label      TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX user_addresses_user_idx ON user_addresses (user_id, created_at);
CREATE UNIQUE INDEX user_addresses_one_home_or_work_key ON user_addresses (user_id, kind) WHERE kind IN ('home', 'work');
