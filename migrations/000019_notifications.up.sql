CREATE TABLE notifications (
    id                  BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id             UUID,
    channel             TEXT        NOT NULL CHECK (channel IN ('fcm', 'sms')),
    template            TEXT        NOT NULL,
    status              TEXT        NOT NULL CHECK (status IN ('sent', 'failed', 'skipped')),
    provider_message_id TEXT        NOT NULL DEFAULT '',
    error               TEXT        NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC);
CREATE INDEX notifications_date_idx ON notifications (created_at);
