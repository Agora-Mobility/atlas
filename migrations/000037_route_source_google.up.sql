-- 'mapbox' stays allowed: older quotes really came from Mapbox.
ALTER TABLE fare_estimates DROP CONSTRAINT fare_estimates_route_source_check;
ALTER TABLE fare_estimates ADD CONSTRAINT fare_estimates_route_source_check CHECK (route_source IN ('mapbox', 'google', 'fallback'));
