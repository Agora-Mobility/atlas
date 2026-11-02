INSERT INTO platform_settings (key, value) VALUES ('prepay_hold_minutes', '60');

CREATE INDEX service_requests_prepay_hold_idx ON service_requests (created_at)
    WHERE prepay AND status = 'scheduled' AND payment_status = 'unpaid';
