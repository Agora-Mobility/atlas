ALTER TABLE service_requests DROP COLUMN completion_attempts, DROP COLUMN completion_code, DROP COLUMN vehicle_type_id;
ALTER TABLE fare_estimates DROP COLUMN vehicle_type_id;
ALTER TABLE dispatch_policies DROP COLUMN requires_completion_code;
DELETE FROM platform_settings WHERE key IN ('towing_code_digits', 'completion_code_attempts');
DELETE FROM catalog_items WHERE kind IN ('towing_vehicle', 'towing_incident');
