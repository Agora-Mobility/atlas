-- The navigation app the driver opens from a ride ('' until they choose one).
ALTER TABLE users
    ADD COLUMN navigation_app TEXT NOT NULL DEFAULT ''
        CHECK (navigation_app IN ('', 'google_maps', 'waze', 'apple_maps'));
