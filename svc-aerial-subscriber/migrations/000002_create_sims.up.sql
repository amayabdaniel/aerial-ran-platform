SET search_path TO subscriber, public;

-- SIM cards we own. Ki/OPc are stored encrypted-at-rest in production via Vault.
-- For Phase 0 we store them as TEXT (lab scope only); a future migration
-- moves them to a Vault transit-key-encrypted column.
CREATE TABLE IF NOT EXISTS sims (
    id              UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id          UUID         NOT NULL,
    owner_user_id   UUID,
    imsi            TEXT         NOT NULL UNIQUE,
    msisdn          TEXT,
    plmn_mcc        TEXT         NOT NULL,
    plmn_mnc        TEXT         NOT NULL,
    ki              TEXT         NOT NULL,            -- 32 hex chars (16 bytes)
    opc             TEXT         NOT NULL,            -- 32 hex chars (16 bytes)
    amf             TEXT         NOT NULL DEFAULT '8000',
    apn             TEXT         NOT NULL DEFAULT 'internet',
    sst             SMALLINT     NOT NULL DEFAULT 1,
    sd              TEXT,
    status          TEXT         NOT NULL DEFAULT 'active',  -- active|suspended|terminated
    provisioned_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sims_owner_idx ON sims(owner_user_id);
CREATE INDEX IF NOT EXISTS sims_org_idx   ON sims(org_id);
CREATE INDEX IF NOT EXISTS sims_status_idx ON sims(status);
