ALTER TABLE providers
    ADD COLUMN province     TEXT NOT NULL DEFAULT '',
    ADD COLUMN municipality TEXT NOT NULL DEFAULT '';
