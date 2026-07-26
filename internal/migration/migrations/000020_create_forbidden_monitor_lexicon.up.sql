-- 违禁监控词库迁移到插件自有业务表：小型配置只保留有限标量，会增长的词条与组合规则改用可分页管理的业务表。
CREATE TABLE forbidden_monitor_terms (
    id BIGSERIAL PRIMARY KEY,
    kind VARCHAR(8) NOT NULL,
    text TEXT NOT NULL,
    weight DOUBLE PRECISION NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_forbidden_terms_kind CHECK (kind IN ('hard', 'risk', 'safe')),
    CONSTRAINT chk_forbidden_terms_text CHECK (length(text) BETWEEN 1 AND 100 AND length(btrim(text)) >= 1),
    CONSTRAINT chk_forbidden_terms_weight CHECK (weight >= 0 AND weight <= 1000),
    CONSTRAINT chk_forbidden_terms_version CHECK (version > 0),
    CONSTRAINT uq_forbidden_terms_kind_text UNIQUE (kind, text)
);

CREATE INDEX idx_forbidden_terms_kind ON forbidden_monitor_terms (kind, id);

CREATE TABLE forbidden_monitor_combinations (
    id BIGSERIAL PRIMARY KEY,
    terms TEXT[] NOT NULL,
    bonus DOUBLE PRECISION NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_forbidden_combinations_terms CHECK (array_length(terms, 1) BETWEEN 2 AND 8),
    CONSTRAINT chk_forbidden_combinations_bonus CHECK (bonus >= 0 AND bonus <= 1000),
    CONSTRAINT chk_forbidden_combinations_version CHECK (version > 0)
);
