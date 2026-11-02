INSERT INTO catalog_items (kind, category_id, code, label, description, amount_cents, attrs, display_order)
SELECT r.kind, c.id, r.code, r.label, r.description, r.cents, r.attrs::jsonb, r.position
FROM service_categories c
JOIN (VALUES
    ('removal_item', 'sofa', 'Sofá', 'Sala', 0, '{"volume_dm3": 1500}', 1),
    ('removal_item', 'tv', 'Televisão', 'Sala', 0, '{"volume_dm3": 200}', 2),
    ('removal_item', 'table', 'Mesa de jantar', 'Sala', 0, '{"volume_dm3": 800}', 3),
    ('removal_item', 'bed', 'Cama de casal', 'Quarto', 0, '{"volume_dm3": 1600}', 4),
    ('removal_item', 'wardrobe', 'Guarda-roupa', 'Quarto', 0, '{"volume_dm3": 1800}', 5),
    ('removal_item', 'fridge', 'Frigorífico', 'Cozinha', 0, '{"volume_dm3": 700}', 6),
    ('removal_item', 'stove', 'Fogão', 'Cozinha', 0, '{"volume_dm3": 400}', 7),
    ('removal_item', 'washer', 'Máquina de lavar', 'Cozinha', 0, '{"volume_dm3": 400}', 8),
    ('removal_item', 'desk', 'Secretária', 'Escritório', 0, '{"volume_dm3": 600}', 9),
    ('removal_item', 'box', 'Caixa', 'Outros', 0, '{"volume_dm3": 100}', 10),
    ('removal_vehicle', 'van', 'Carrinha', 'Até 6 m³', 4500000, '{"max_volume_dm3": 6000, "base_minutes": 120, "minutes_per_m3": 10, "daily_capacity": 6}', 1),
    ('removal_vehicle', 'truck_3t', 'Camião 3,5 t', 'Até 18 m³', 8500000, '{"max_volume_dm3": 18000, "base_minutes": 180, "minutes_per_m3": 10, "daily_capacity": 4}', 2),
    ('removal_vehicle', 'truck_7t', 'Camião 7 t', 'Até 35 m³', 14000000, '{"max_volume_dm3": 35000, "base_minutes": 240, "minutes_per_m3": 10, "daily_capacity": 2}', 3),
    ('removal_extra', 'packing', 'Embalagem', '', 1500000, '{}', 1),
    ('removal_extra', 'assembly', 'Desmontar e montar', '', 1000000, '{}', 2),
    ('removal_extra', 'floors', 'Proteção de pavimentos', '', 500000, '{}', 3),
    ('removal_window', 'morning', '08:00 às 10:00', '', 0, '{"start_minute": 480, "end_minute": 600}', 1),
    ('removal_window', 'midday', '11:00 às 13:00', '', 0, '{"start_minute": 660, "end_minute": 780}', 2),
    ('removal_window', 'afternoon', '14:00 às 16:00', '', 0, '{"start_minute": 840, "end_minute": 960}', 3),
    ('removal_property', 'home', 'Casa', '', 0, '{}', 1),
    ('removal_property', 'office', 'Escritório ou loja', '', 0, '{}', 2)
) AS r (kind, code, label, description, cents, attrs, position) ON c.slug = 'removals';

INSERT INTO platform_settings (key, value) VALUES
    ('removal_helper_cents', '500000'),
    ('removal_insurance_cents', '50000000'),
    ('removal_max_helpers', '6'),
    ('removal_max_days', '60');

UPDATE dispatch_policies
SET offer_mode = 'parallel', pricing_mode = 'fixed', ranking = 'base_distance'
WHERE category_id = (SELECT id FROM service_categories WHERE slug = 'removals');

ALTER TABLE fare_estimates ADD COLUMN details JSONB NOT NULL DEFAULT '{}';

ALTER TABLE service_requests
    ADD COLUMN removal_day DATE,
    ADD COLUMN removal_window_id UUID REFERENCES catalog_items (id);

CREATE INDEX service_requests_removal_day_idx ON service_requests (vehicle_type_id, removal_day)
    WHERE removal_day IS NOT NULL AND status NOT IN ('cancelled', 'expired');

CREATE TABLE request_team_members (
    id         UUID        PRIMARY KEY,
    request_id UUID        NOT NULL REFERENCES service_requests (id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    role       TEXT        NOT NULL CHECK (role IN ('lead', 'helper')),
    verified   BOOLEAN     NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX request_team_members_request_idx ON request_team_members (request_id, created_at);
