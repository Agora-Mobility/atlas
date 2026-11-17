ALTER TABLE notifications ADD COLUMN delivery_key TEXT;

CREATE UNIQUE INDEX notifications_delivered_key ON notifications (delivery_key) WHERE status = 'sent' AND delivery_key IS NOT NULL;
