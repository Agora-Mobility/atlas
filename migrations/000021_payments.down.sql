DROP TABLE payment_refunds;
DROP TABLE payment_events;
DROP TABLE payments;
DROP TABLE category_payment_methods;
DROP INDEX service_requests_outstanding_idx;
ALTER TABLE service_requests DROP COLUMN payment_status, DROP COLUMN payment_method;
