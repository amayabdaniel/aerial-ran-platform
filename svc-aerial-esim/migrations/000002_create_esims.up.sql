SET search_path TO esim, public;

-- An eSIM we issued to a user — either backed by a real provider (Airalo, EMnify, …)
-- or by a deterministic mock (no API keys).
CREATE TABLE IF NOT EXISTS esims (
    id              UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id          UUID         NOT NULL,
    owner_user_id   UUID,
    provider        TEXT         NOT NULL,                  -- airalo|emnify|mock
    provider_ref    TEXT,                                   -- provider's order/sim id
    iccid           TEXT,                                   -- 19-20 digit, optional until provider returns it
    package_id      TEXT,                                   -- provider package sku
    package_label   TEXT,
    data_mb         INTEGER,                                -- e.g. 5120 = 5 GB
    validity_days   INTEGER,                                -- e.g. 30
    lpa_string      TEXT,                                   -- LPA:1$rsp.airalo.com$ABC123
    qr_png_b64      TEXT,                                   -- inline QR for the LPA
    install_url     TEXT,                                   -- iOS 17.4+ Universal Link
    status          TEXT         NOT NULL DEFAULT 'ordered', -- ordered|active|expired|cancelled
    activated_at    TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    last_usage_mb   INTEGER      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS esims_owner_idx ON esims(owner_user_id);
CREATE INDEX IF NOT EXISTS esims_org_idx   ON esims(org_id);
CREATE INDEX IF NOT EXISTS esims_status_idx ON esims(status);

-- Cached catalog from the provider (refreshable).
CREATE TABLE IF NOT EXISTS packages (
    id              TEXT         PRIMARY KEY,               -- provider sku
    provider        TEXT         NOT NULL,
    label           TEXT         NOT NULL,
    region          TEXT,                                   -- country code or region
    data_mb         INTEGER      NOT NULL,
    validity_days   INTEGER      NOT NULL,
    price_usd_cents INTEGER      NOT NULL,
    raw             JSONB,                                  -- provider raw payload for forensics
    fetched_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);
