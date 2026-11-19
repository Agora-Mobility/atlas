-- Tokens the customer app registers for one request, so the lock screen can follow
-- the ride: the FCM token on Android, the push token of the Live Activity on iOS.
-- One per platform; they are deleted when the ride ends.
CREATE TABLE ride_push_tokens (
    request_id UUID        NOT NULL REFERENCES service_requests (id) ON DELETE CASCADE,
    platform   TEXT        NOT NULL CHECK (platform IN ('android', 'ios')),
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token      TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (request_id, platform)
);

CREATE INDEX ride_push_tokens_token_idx ON ride_push_tokens (token);

-- Live Activity pushes go straight to APNs and are recorded like the others.
ALTER TABLE notifications DROP CONSTRAINT notifications_channel_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_channel_check CHECK (channel IN ('fcm', 'sms', 'apns'));
