DROP INDEX service_requests_customer_active_idx;
ALTER TABLE service_categories DROP COLUMN passenger_sms;
ALTER TABLE fare_estimates DROP COLUMN used_at, DROP COLUMN scheduled_for;
ALTER TABLE service_tariffs DROP COLUMN stop_fee_cents;
ALTER TABLE dispatch_policies DROP COLUMN schedule_slot_minutes, DROP COLUMN max_stops;
