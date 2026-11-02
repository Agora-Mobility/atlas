DELETE FROM platform_settings WHERE key IN ('booking_max_days', 'booking_slot_minutes');
DELETE FROM catalog_items WHERE kind = 'workshop_category' AND code IN ('review', 'oil', 'tires', 'brakes', 'electric', 'bodywork');
UPDATE services SET description = 'Um técnico desloca-se ao local indicado.' WHERE slug = 'maintenance';
UPDATE category_payment_methods SET is_enabled = true WHERE method = 'cash' AND category_id = (SELECT id FROM service_categories WHERE slug = 'maintenance');
UPDATE service_categories SET code_prefix = 'MN' WHERE slug = 'maintenance';
ALTER TABLE dispatch_policies DROP COLUMN booking_only;
DELETE FROM request_messages WHERE sender_type = 'workshop';
ALTER TABLE request_messages DROP CONSTRAINT request_messages_sender_type_check;
ALTER TABLE request_messages ADD CONSTRAINT request_messages_sender_type_check CHECK (sender_type IN ('customer', 'provider'));

ALTER TABLE ledger_entries DROP CONSTRAINT ledger_entries_check;
ALTER TABLE ledger_entries DROP CONSTRAINT ledger_entries_account_type_check;
ALTER TABLE ledger_entries
    ADD CONSTRAINT ledger_entries_account_type_check CHECK (account_type IN ('provider', 'wallet', 'platform', 'external')),
    ADD CONSTRAINT ledger_entries_check CHECK ((account_type IN ('provider', 'wallet')) = (account_id IS NOT NULL));

ALTER TABLE payments DROP CONSTRAINT payments_kind_check;
ALTER TABLE payments ADD CONSTRAINT payments_kind_check CHECK (kind IN ('fare', 'cancellation_fee', 'tip'));

ALTER TABLE ratings DROP COLUMN workshop_id;
DROP TABLE booking_events;
DROP TABLE booking_extras;
DROP TABLE booking_items;
DROP TABLE bookings;
ALTER TABLE service_requests DROP COLUMN extra_due_cents, DROP COLUMN prepay, DROP COLUMN workshop_id;
ALTER TABLE vouchers DROP CONSTRAINT vouchers_auto_apply_workshop, DROP COLUMN auto_apply, DROP COLUMN workshop_service_id, DROP COLUMN workshop_id;
DROP TABLE workshop_favorites;
DROP TABLE workshop_mechanics;
DROP TABLE workshop_closures;
DROP TABLE workshop_hours;
ALTER TABLE workshop_services DROP COLUMN category_id;
ALTER TABLE workshops DROP COLUMN rating_count, DROP COLUMN rating_sum, DROP COLUMN warranty, DROP COLUMN about, DROP COLUMN capacity;
