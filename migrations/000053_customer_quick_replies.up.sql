UPDATE catalog_items SET code = 'customer_waiting', label = 'Já estou à sua espera', display_order = 1, updated_at = now() WHERE kind = 'quick_reply' AND code = 'customer_here';
UPDATE catalog_items SET code = 'customer_eta', label = 'Quanto tempo falta?', display_order = 2, updated_at = now() WHERE kind = 'quick_reply' AND code = 'customer_coming';
UPDATE catalog_items SET code = 'customer_seen', label = 'Já vi o carro, vou já', display_order = 3, updated_at = now() WHERE kind = 'quick_reply' AND code = 'customer_wait';
