-- 引导占位表，用于验证迁移链路。
-- 业务表将在后续 spec 中通过成对的 up/down 迁移新增。
CREATE TABLE system_meta (
    key        TEXT        PRIMARY KEY,
    value      TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
