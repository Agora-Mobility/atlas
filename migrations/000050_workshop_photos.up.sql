CREATE TABLE workshop_photos (
    upload_id   UUID        PRIMARY KEY REFERENCES media_uploads (id),
    workshop_id UUID        NOT NULL REFERENCES workshops (id) ON DELETE CASCADE,
    position    INTEGER     NOT NULL CHECK (position >= 0),
    created_at  TIMESTAMPTZ NOT NULL,
    CONSTRAINT workshop_photos_position_key UNIQUE (workshop_id, position)
);

INSERT INTO platform_settings (key, value) VALUES ('max_workshop_photos', '10');
