ALTER TABLE organizations
    ADD COLUMN nif TEXT,
    ADD CONSTRAINT organizations_nif_format_chk CHECK (nif ~ '^([0-9]{10}|[0-9]{9}[A-Z]{2}[0-9]{3})$');

CREATE UNIQUE INDEX organizations_nif_key ON organizations (nif) WHERE nif IS NOT NULL;
