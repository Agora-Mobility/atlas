-- Reference module that exercises every layer (REST → usecase → SQLC →
-- PostGIS → outbox). Remove once real modules exist.
CREATE TABLE examples (
    id          UUID                  PRIMARY KEY,
    name        TEXT                  NOT NULL,
    description TEXT                  NOT NULL DEFAULT '',
    location    geometry(Point, 4326) NOT NULL,
    created_at  TIMESTAMPTZ           NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ           NOT NULL DEFAULT now()
);

CREATE INDEX examples_location_idx ON examples USING GIST (location);
CREATE INDEX examples_created_at_idx ON examples (created_at DESC);
