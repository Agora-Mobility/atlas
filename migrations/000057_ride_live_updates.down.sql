DELETE FROM notifications WHERE channel = 'apns';
ALTER TABLE notifications DROP CONSTRAINT notifications_channel_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_channel_check CHECK (channel IN ('fcm', 'sms'));

DROP TABLE ride_push_tokens;
