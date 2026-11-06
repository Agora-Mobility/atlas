DROP TABLE request_team_members;
DROP INDEX service_requests_removal_day_idx;
ALTER TABLE service_requests DROP COLUMN removal_window_id, DROP COLUMN removal_day;
ALTER TABLE fare_estimates DROP COLUMN details;
UPDATE dispatch_policies
SET offer_mode = 'quote', pricing_mode = 'quote', ranking = 'customer_choice'
WHERE category_id = (SELECT id FROM service_categories WHERE slug = 'removals');
DELETE FROM platform_settings WHERE key IN ('removal_helper_cents', 'removal_insurance_cents', 'removal_max_helpers', 'removal_max_days');
DELETE FROM catalog_items WHERE kind IN ('removal_item', 'removal_vehicle', 'removal_extra', 'removal_window', 'removal_property');
