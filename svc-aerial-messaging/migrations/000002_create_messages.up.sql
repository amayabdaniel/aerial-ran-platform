SET search_path TO messaging, public;

-- Append-only message log. Subjects are JetStream-side; we keep a search/history table here.
CREATE TABLE IF NOT EXISTS messages (
    id           UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id       UUID         NOT NULL,
    from_user_id UUID         NOT NULL,
    to_user_id   UUID         NOT NULL,
    body         TEXT         NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS msg_to_idx ON messages(to_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS msg_from_idx ON messages(from_user_id, created_at DESC);
