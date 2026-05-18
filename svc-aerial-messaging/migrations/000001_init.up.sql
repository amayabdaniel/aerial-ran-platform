SET search_path TO messaging, public;
CREATE TABLE IF NOT EXISTS _bootstrap (
  bootstrapped_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
