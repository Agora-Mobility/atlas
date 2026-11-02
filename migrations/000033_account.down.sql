DELETE FROM catalog_items WHERE kind = 'help_topic' AND code IN ('payments', 'cancel', 'safety', 'bookings', 'removals', 'account');
DELETE FROM platform_settings WHERE key IN ('referral_reward_cents', 'support_phone');
DROP TABLE referrals;
DROP TABLE inbox_items;
ALTER TABLE users
    DROP COLUMN referral_code,
    DROP COLUMN avatar_upload_id,
    DROP COLUMN currency,
    DROP COLUMN language,
    DROP COLUMN share_location,
    DROP COLUMN notify_sms,
    DROP COLUMN notify_promotions,
    DROP COLUMN notify_status;
