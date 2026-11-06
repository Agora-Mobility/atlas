-- The old constraint has no value for Google quotes. 'fallback' is the closest: not a Mapbox route.
UPDATE fare_estimates SET route_source = 'fallback' WHERE route_source = 'google';
ALTER TABLE fare_estimates DROP CONSTRAINT fare_estimates_route_source_check;
ALTER TABLE fare_estimates ADD CONSTRAINT fare_estimates_route_source_check CHECK (route_source IN ('mapbox', 'fallback'));
