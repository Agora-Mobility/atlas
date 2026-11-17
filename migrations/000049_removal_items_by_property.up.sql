INSERT INTO platform_settings (key, value) VALUES
    ('removal_custom_small_dm3', '100'),
    ('removal_custom_medium_dm3', '600'),
    ('removal_custom_large_dm3', '1500');

UPDATE catalog_items i
SET attrs = i.attrs || '{"properties": ["home"]}'::jsonb, updated_at = now()
FROM service_categories c
WHERE i.category_id = c.id AND c.slug = 'removals' AND i.kind = 'removal_item'
  AND i.code IN ('bed', 'wardrobe', 'stove', 'washer');
