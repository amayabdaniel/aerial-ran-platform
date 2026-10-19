SET search_path TO provision, public;

-- Enforce at most one NON-cancelled subscription per (user_id, plan_id). The
-- INSERT path never checked this, so a user could accumulate duplicate active
-- subscriptions to the same plan (the ErrSubExists sentinel existed but nothing
-- could ever raise it). A PARTIAL unique index — scoped to status <> 'cancelled'
-- — is the right shape: it blocks duplicate live subscriptions but still lets a
-- user re-subscribe to a plan they previously cancelled.
CREATE UNIQUE INDEX IF NOT EXISTS subscriptions_user_plan_active_uniq
    ON subscriptions (user_id, plan_id)
    WHERE status <> 'cancelled';
