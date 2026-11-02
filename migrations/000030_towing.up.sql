INSERT INTO catalog_items (kind, category_id, code, label, description, amount_cents, attrs, display_order)
SELECT r.kind, c.id, r.code, r.label, r.description, r.cents, r.attrs::jsonb, r.position
FROM service_categories c
JOIN (VALUES
    ('towing_vehicle', 'light', 'Ligeiro', 'Viatura ligeira', 2500000, '{"included_km": 10, "extra_km_cents": 50000, "min_capacity_kg": 0}', 1),
    ('towing_vehicle', 'suv_pickup', 'SUV/Pick-up', 'SUV ou pick-up', 3200000, '{"included_km": 10, "extra_km_cents": 60000, "min_capacity_kg": 2500}', 2),
    ('towing_vehicle', 'motorcycle', 'Motociclo', 'Motociclo', 1500000, '{"included_km": 10, "extra_km_cents": 30000, "min_capacity_kg": 0}', 3),
    ('towing_incident', 'breakdown', 'Avaria mecânica', '', 0, '{}', 1),
    ('towing_incident', 'accident', 'Acidente', '', 0, '{}', 2),
    ('towing_incident', 'flat_tire', 'Pneu furado', '', 0, '{}', 3),
    ('towing_incident', 'battery', 'Bateria descarregada', '', 0, '{}', 4),
    ('towing_incident', 'no_fuel', 'Sem combustível', '', 0, '{}', 5),
    ('towing_incident', 'other', 'Outro', '', 0, '{}', 99)
) AS r (kind, code, label, description, cents, attrs, position) ON c.slug = 'towing';

INSERT INTO platform_settings (key, value) VALUES ('towing_code_digits', '4'), ('completion_code_attempts', '5');

ALTER TABLE dispatch_policies ADD COLUMN requires_completion_code BOOLEAN NOT NULL DEFAULT false;
UPDATE dispatch_policies SET requires_completion_code = true WHERE category_id = (SELECT id FROM service_categories WHERE slug = 'towing');

ALTER TABLE fare_estimates ADD COLUMN vehicle_type_id UUID REFERENCES catalog_items (id);

ALTER TABLE service_requests
    ADD COLUMN vehicle_type_id UUID REFERENCES catalog_items (id),
    ADD COLUMN completion_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN completion_attempts SMALLINT NOT NULL DEFAULT 0 CHECK (completion_attempts >= 0);
