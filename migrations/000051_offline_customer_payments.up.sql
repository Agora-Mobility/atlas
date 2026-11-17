UPDATE category_payment_methods SET is_enabled = (method = 'cash'), updated_at = now();

UPDATE cancellation_rules SET fee_cents = 0, updated_at = now();
