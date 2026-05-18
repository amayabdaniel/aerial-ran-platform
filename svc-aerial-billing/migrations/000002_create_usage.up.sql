SET search_path TO billing, public;

-- Raw usage events ingested from svc-aerial-esim or future CDR sources.
CREATE TABLE IF NOT EXISTS usage_events (
    id          BIGSERIAL    PRIMARY KEY,
    org_id      UUID         NOT NULL,
    user_id     UUID,
    source      TEXT         NOT NULL,                 -- esim|cdr|manual
    resource_id TEXT,                                  -- esim.id, sim.imsi, etc.
    data_mb     INTEGER      NOT NULL DEFAULT 0,
    minutes     INTEGER      NOT NULL DEFAULT 0,
    sms_count   INTEGER      NOT NULL DEFAULT 0,
    cents       INTEGER      NOT NULL DEFAULT 0,       -- if pre-priced
    occurred_at TIMESTAMPTZ  NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS usage_user_idx ON usage_events(user_id, occurred_at);
CREATE INDEX IF NOT EXISTS usage_org_idx  ON usage_events(org_id,  occurred_at);

-- Monthly rollup, accumulated via UPSERT.
-- Note: we coerce a null user_id to the all-zero UUID at write-time so the
-- composite primary key works (Postgres can't have expressions in PK).
CREATE TABLE IF NOT EXISTS usage_rollups (
    org_id      UUID         NOT NULL,
    user_id     UUID         NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000'::uuid,
    month       DATE         NOT NULL,
    data_mb     INTEGER      NOT NULL DEFAULT 0,
    minutes     INTEGER      NOT NULL DEFAULT 0,
    sms_count   INTEGER      NOT NULL DEFAULT 0,
    cents       INTEGER      NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id, month)
);
