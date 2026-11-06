DROP INDEX service_requests_prepay_hold_idx;
DELETE FROM platform_settings WHERE key = 'prepay_hold_minutes';
