CREATE TABLE request_trip_metrics (
    request_id             UUID        PRIMARY KEY REFERENCES service_requests (id),
    estimated_distance_m   INTEGER     NOT NULL,
    estimated_duration_s   INTEGER     NOT NULL,
    actual_distance_m      INTEGER     NOT NULL,
    actual_duration_s      INTEGER     NOT NULL,
    distance_deviation_bps INTEGER     NOT NULL,
    duration_deviation_bps INTEGER     NOT NULL,
    samples                INTEGER     NOT NULL DEFAULT 0,
    route_changed          BOOLEAN     NOT NULL DEFAULT false,
    needs_review           BOOLEAN     NOT NULL DEFAULT false,
    recorded_at            TIMESTAMPTZ NOT NULL
);

CREATE INDEX request_trip_metrics_review_idx ON request_trip_metrics (recorded_at DESC) WHERE needs_review;
