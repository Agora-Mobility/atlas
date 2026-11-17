UPDATE catalog_items SET code = 'customer_here', label = 'Já estou no local', display_order = 2, updated_at = now() WHERE kind = 'quick_reply' AND code = 'customer_waiting';
UPDATE catalog_items SET code = 'customer_coming', label = 'Estou a chegar', display_order = 1, updated_at = now() WHERE kind = 'quick_reply' AND code = 'customer_eta';
UPDATE catalog_items SET code = 'customer_wait', label = 'Aguarde um momento, por favor', display_order = 3, updated_at = now() WHERE kind = 'quick_reply' AND code = 'customer_seen';
