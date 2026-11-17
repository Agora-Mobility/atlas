ALTER TABLE service_requests ADD COLUMN pickup_h3_r9 BIGINT;

CREATE INDEX service_requests_h3_r9_idx ON service_requests (pickup_h3_r9, created_at);
CREATE INDEX service_requests_created_idx ON service_requests (created_at);
