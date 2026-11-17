DROP INDEX service_requests_created_idx;
DROP INDEX service_requests_h3_r9_idx;
ALTER TABLE service_requests DROP COLUMN pickup_h3_r9;
