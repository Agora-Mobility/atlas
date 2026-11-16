UPDATE catalog_items SET attrs = attrs - 'properties' WHERE kind = 'removal_item';

DELETE FROM platform_settings WHERE key IN ('removal_custom_small_dm3', 'removal_custom_medium_dm3', 'removal_custom_large_dm3');
