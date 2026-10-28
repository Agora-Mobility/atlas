CREATE TYPE user_role AS ENUM ('customer', 'provider', 'operator', 'admin', 'partner');
CREATE TYPE user_status AS ENUM ('active', 'inactive', 'blocked');

CREATE TABLE users (
    id            UUID        PRIMARY KEY,
    name          TEXT        NOT NULL,
    email         TEXT,
    password      TEXT        NOT NULL,
    phone         TEXT,
    is_verified   BOOLEAN     NOT NULL DEFAULT false,
    status        user_status NOT NULL DEFAULT 'active',
    role          user_role   NOT NULL,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ,
    CONSTRAINT users_email_or_phone_chk CHECK (email IS NOT NULL OR phone IS NOT NULL)
);

CREATE UNIQUE INDEX users_email_key ON users (lower(email)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX users_phone_key ON users (phone) WHERE deleted_at IS NULL;
CREATE INDEX users_role_status_idx ON users (role, status) WHERE deleted_at IS NULL;
