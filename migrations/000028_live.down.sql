DELETE FROM catalog_items WHERE kind = 'quick_reply';
DELETE FROM platform_settings WHERE key = 'share_ttl_minutes';
DROP TABLE sos_alerts;
DROP TABLE request_shares;
DROP TABLE request_messages;
