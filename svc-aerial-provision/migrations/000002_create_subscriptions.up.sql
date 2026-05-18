SET search_path TO provision, public;

CREATE TABLE IF NOT EXISTS plans (
    id              TEXT         PRIMARY KEY,
    name            TEXT         NOT NULL,
    monthly_cents   INTEGER      NOT NULL,
    data_cap_mb     INTEGER,
    description     TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id              UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id          UUID         NOT NULL,
    user_id         UUID         NOT NULL,
    plan_id         TEXT         NOT NULL REFERENCES plans(id),
    sim_id          UUID,
    esim_id         UUID,
    status          TEXT         NOT NULL DEFAULT 'active',  -- active|suspended|cancelled
    started_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    cancelled_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS sub_user_idx ON subscriptions(user_id);
CREATE INDEX IF NOT EXISTS sub_org_idx  ON subscriptions(org_id);

-- Seed three default plans for family use.
INSERT INTO plans(id, name, monthly_cents, data_cap_mb, description) VALUES
  ('aerial-basic',   'Aerial Basic',    500,  2048,  'Voice/SMS + 2 GB data'),
  ('aerial-family',  'Aerial Family',   1500, 10240, 'Unlimited voice/SMS + 10 GB data'),
  ('aerial-premium', 'Aerial Premium',  2500, 0,     'Unlimited everything')
ON CONFLICT (id) DO NOTHING;
