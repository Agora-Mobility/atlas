CREATE TABLE platform_settings (
    key        TEXT        PRIMARY KEY,
    value      TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO platform_settings (key, value) VALUES ('max_tip_cents', '5000000');

CREATE TABLE catalog_items (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    kind               TEXT        NOT NULL CHECK (kind IN ('compliment', 'tip_preset', 'quick_reply', 'towing_vehicle', 'towing_incident', 'removal_item', 'removal_vehicle', 'removal_extra', 'removal_window', 'removal_property', 'help_topic', 'workshop_category')),
    category_id        UUID        REFERENCES service_categories (id),
    code               TEXT        NOT NULL,
    label              TEXT        NOT NULL,
    description        TEXT        NOT NULL DEFAULT '',
    amount_cents       BIGINT      NOT NULL DEFAULT 0 CHECK (amount_cents >= 0),
    promo_amount_cents BIGINT      CHECK (promo_amount_cents >= 0),
    promo_until        TIMESTAMPTZ,
    attrs              JSONB       NOT NULL DEFAULT '{}',
    display_order      INTEGER     NOT NULL DEFAULT 0 CHECK (display_order BETWEEN 0 AND 9999),
    is_active          BOOLEAN     NOT NULL DEFAULT true,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX catalog_items_code_key ON catalog_items (kind, code, COALESCE(category_id, '00000000-0000-0000-0000-000000000000'::uuid));
CREATE INDEX catalog_items_kind_idx ON catalog_items (kind, category_id, display_order);

INSERT INTO catalog_items (kind, category_id, code, label, amount_cents, display_order)
SELECT r.kind, c.id, r.code, r.label, r.cents, r.position
FROM service_categories c
JOIN (VALUES
    ('taxi', 'compliment', 'safe_driving', 'Condução segura', 0, 1),
    ('taxi', 'compliment', 'clean_car', 'Viatura limpa', 0, 2),
    ('taxi', 'compliment', 'friendly', 'Simpático', 0, 3),
    ('taxi', 'compliment', 'on_time', 'Pontual', 0, 4),
    ('towing', 'compliment', 'fast', 'Chegou rápido', 0, 1),
    ('towing', 'compliment', 'careful', 'Cuidadoso', 0, 2),
    ('towing', 'compliment', 'professional', 'Profissional', 0, 3),
    ('towing', 'compliment', 'communication', 'Boa comunicação', 0, 4),
    ('removals', 'compliment', 'on_time', 'Pontuais', 0, 1),
    ('removals', 'compliment', 'careful', 'Cuidadosos', 0, 2),
    ('removals', 'compliment', 'fast', 'Rápidos', 0, 3),
    ('removals', 'compliment', 'friendly', 'Simpáticos', 0, 4),
    ('maintenance', 'compliment', 'good_work', 'Trabalho bem feito', 0, 1),
    ('maintenance', 'compliment', 'fair_price', 'Preço justo', 0, 2),
    ('maintenance', 'compliment', 'on_time', 'Cumpriu o prazo', 0, 3),
    ('maintenance', 'compliment', 'attention', 'Bom atendimento', 0, 4),
    ('taxi', 'tip_preset', 'tip_200', '200 Kz', 20000, 1),
    ('taxi', 'tip_preset', 'tip_500', '500 Kz', 50000, 2),
    ('taxi', 'tip_preset', 'tip_1000', '1 000 Kz', 100000, 3),
    ('towing', 'tip_preset', 'tip_500', '500 Kz', 50000, 1),
    ('towing', 'tip_preset', 'tip_1000', '1 000 Kz', 100000, 2),
    ('towing', 'tip_preset', 'tip_2000', '2 000 Kz', 200000, 3),
    ('removals', 'tip_preset', 'tip_2000', '2 000 Kz', 200000, 1),
    ('removals', 'tip_preset', 'tip_5000', '5 000 Kz', 500000, 2),
    ('removals', 'tip_preset', 'tip_10000', '10 000 Kz', 1000000, 3),
    ('maintenance', 'tip_preset', 'tip_500', '500 Kz', 50000, 1),
    ('maintenance', 'tip_preset', 'tip_1000', '1 000 Kz', 100000, 2),
    ('maintenance', 'tip_preset', 'tip_2000', '2 000 Kz', 200000, 3)
) AS r (category_slug, kind, code, label, cents, position) ON r.category_slug = c.slug;

CREATE TABLE ratings (
    id             UUID        PRIMARY KEY,
    request_id     UUID        UNIQUE REFERENCES service_requests (id),
    customer_id    UUID        NOT NULL REFERENCES users (id),
    provider_id    UUID        REFERENCES users (id),
    stars          SMALLINT    NOT NULL CHECK (stars BETWEEN 1 AND 5),
    compliment_ids UUID[]      NOT NULL DEFAULT '{}',
    comment        TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL
);

CREATE INDEX ratings_provider_idx ON ratings (provider_id, created_at DESC) WHERE provider_id IS NOT NULL;

ALTER TABLE providers
    ADD COLUMN rating_sum BIGINT NOT NULL DEFAULT 0 CHECK (rating_sum >= 0),
    ADD COLUMN rating_count INTEGER NOT NULL DEFAULT 0 CHECK (rating_count >= 0),
    ADD COLUMN completed_requests INTEGER NOT NULL DEFAULT 0 CHECK (completed_requests >= 0);

UPDATE providers p SET completed_requests = c.total
FROM (SELECT provider_id, count(*) AS total FROM service_requests WHERE status = 'completed' AND provider_id IS NOT NULL GROUP BY provider_id) c
WHERE c.provider_id = p.user_id;
