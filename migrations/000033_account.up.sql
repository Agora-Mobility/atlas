ALTER TABLE users
    ADD COLUMN notify_status     BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN notify_promotions BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN notify_sms        BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN share_location    BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN language          TEXT    NOT NULL DEFAULT 'pt' CHECK (language IN ('pt', 'en', 'fr')),
    ADD COLUMN currency          CHAR(3) NOT NULL DEFAULT 'AOA',
    ADD COLUMN avatar_upload_id  UUID REFERENCES media_uploads (id),
    ADD COLUMN referral_code     TEXT UNIQUE;

CREATE TABLE inbox_items (
    id         UUID        PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES users (id),
    kind       TEXT        NOT NULL,
    title      TEXT        NOT NULL,
    body       TEXT        NOT NULL DEFAULT '',
    link_type  TEXT        NOT NULL DEFAULT '',
    link_id    UUID,
    event_id   TEXT        NOT NULL,
    read_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (user_id, event_id)
);

CREATE INDEX inbox_items_user_idx ON inbox_items (user_id, created_at DESC);
CREATE INDEX inbox_items_unread_idx ON inbox_items (user_id) WHERE read_at IS NULL;

CREATE TABLE referrals (
    id           UUID        PRIMARY KEY,
    referrer_id  UUID        NOT NULL REFERENCES users (id),
    referred_id  UUID        NOT NULL UNIQUE REFERENCES users (id),
    status       TEXT        NOT NULL CHECK (status IN ('pending', 'rewarded')),
    reward_cents BIGINT      NOT NULL DEFAULT 0 CHECK (reward_cents >= 0),
    rewarded_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL,
    CHECK (referrer_id <> referred_id)
);

CREATE INDEX referrals_referrer_idx ON referrals (referrer_id, created_at DESC);

INSERT INTO platform_settings (key, value) VALUES ('referral_reward_cents', '100000'), ('support_phone', '+244923000000');

INSERT INTO catalog_items (kind, code, label, description, display_order) VALUES
    ('help_topic', 'payments', 'Pagamentos e reembolsos', 'Como pagar com Multicaixa Express, referência ou Carteira. Os reembolsos de referência vão para a Carteira.', 1),
    ('help_topic', 'cancel', 'Cancelar um pedido', 'Sem motorista atribuído, cancelar não custa nada. O custo é sempre indicado antes de confirmares.', 2),
    ('help_topic', 'safety', 'Segurança', 'Durante a viagem podes partilhar o trajeto e acionar o SOS.', 3),
    ('help_topic', 'bookings', 'Marcações na oficina', 'Só pagas depois de aprovar o serviço concluído. Um orçamento extra só entra no total se o aprovares.', 4),
    ('help_topic', 'removals', 'Mudanças', 'O preço fica fechado na marcação. Cancelar é grátis até 24 horas antes.', 5),
    ('help_topic', 'account', 'A minha conta', 'Altera o perfil, as preferências e os convites nas definições.', 6);
