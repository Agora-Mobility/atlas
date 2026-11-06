DELETE FROM platform_settings WHERE key IN ('max_request_photos', 'max_booking_photos', 'photo_max_bytes', 'photo_content_types');
DROP TABLE media_uploads;
DROP TABLE customer_vehicles;
