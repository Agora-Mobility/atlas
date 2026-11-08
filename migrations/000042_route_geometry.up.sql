ALTER TABLE fare_estimates ADD COLUMN route_geometry TEXT NOT NULL DEFAULT '';
ALTER TABLE service_requests ADD COLUMN route_geometry TEXT NOT NULL DEFAULT '';
