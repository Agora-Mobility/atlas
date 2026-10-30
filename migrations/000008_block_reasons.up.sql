CREATE TABLE block_reasons (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    code       TEXT        NOT NULL,
    label      TEXT        NOT NULL,
    is_active  BOOLEAN     NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX block_reasons_code_key ON block_reasons (code);

CREATE TABLE user_blocks (
    id         UUID        PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES users (id),
    blocked_by UUID        REFERENCES users (id),
    note       TEXT        NOT NULL DEFAULT '',
    blocked_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX user_blocks_user_idx ON user_blocks (user_id, blocked_at DESC);

CREATE TABLE user_block_reasons (
    user_block_id   UUID NOT NULL REFERENCES user_blocks (id) ON DELETE CASCADE,
    block_reason_id UUID NOT NULL REFERENCES block_reasons (id),
    PRIMARY KEY (user_block_id, block_reason_id)
);

CREATE INDEX user_block_reasons_reason_idx ON user_block_reasons (block_reason_id);

INSERT INTO block_reasons (code, label) VALUES
    ('fraud', 'Fraude ou actividade suspeita'),
    ('abusive_behavior', 'Comportamento abusivo ou inadequado'),
    ('false_information', 'Informação ou documentos falsos'),
    ('duplicate_account', 'Conta duplicada'),
    ('payment_issues', 'Pagamentos em falta ou contestados'),
    ('terms_violation', 'Violação dos termos de uso'),
    ('user_request', 'Pedido do próprio utilizador'),
    ('other', 'Outro');
