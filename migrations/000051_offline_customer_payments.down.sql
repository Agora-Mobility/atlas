UPDATE category_payment_methods m
SET is_enabled = CASE m.method
        WHEN 'cash' THEN c.slug <> 'maintenance'
        WHEN 'reference' THEN c.slug IN ('maintenance', 'removals')
        ELSE true
    END,
    updated_at = now()
FROM service_categories c
WHERE c.id = m.category_id;

UPDATE cancellation_rules r
SET fee_cents = d.fee, updated_at = now()
FROM services s
JOIN (VALUES
    ('taxi', 50000),
    ('taxi-comfort', 75000),
    ('taxi-xl', 100000),
    ('towing', 200000),
    ('removals', 1500000)
) AS d (slug, fee) ON d.slug = s.slug
WHERE r.service_id = s.id;
