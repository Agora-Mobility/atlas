DROP TABLE support_messages;
DROP TABLE support_tickets;
DROP SEQUENCE support_ticket_code_seq;
ALTER TABLE service_request_stops DROP COLUMN arrived_at;
DROP INDEX service_requests_public_code_key;
ALTER TABLE service_requests DROP COLUMN public_code;
DROP SEQUENCE service_request_code_seq;
ALTER TABLE service_categories DROP COLUMN code_prefix;
