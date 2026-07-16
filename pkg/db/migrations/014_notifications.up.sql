-- Sprint 1 — Notifications & email verification.
--
-- Three concerns land together because they share the "reach a human" surface:
--   1. in-app notifications (a per-user inbox fed by the domain events that
--      already fire on the live loop: rfq.matched, quote.submitted, order.created);
--   2. per-user notification preferences (email vs in-app, per event type);
--   3. email verification (the first transactional email; gates activation).

-- 1. In-app notifications. One row per recipient user. Domain events are
--    company-scoped, so a single event fans out to every user of the target
--    company. `data` carries the ids needed to deep-link (rfq_id, quote_id, ...).
CREATE TABLE notifications (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    user_id    TEXT NOT NULL REFERENCES users(id),
    company_id TEXT NOT NULL REFERENCES companies(id),
    type       TEXT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL,
    data       JSONB,
    read_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Inbox listing is newest-first per user; the unread count filters on read_at.
CREATE INDEX idx_notifications_user ON notifications (user_id, created_at DESC);
CREATE INDEX idx_notifications_user_unread ON notifications (user_id) WHERE read_at IS NULL;

-- 2. Per-user, per-event-type delivery preferences. A missing row means "use the
--    default" (both channels on), so this table only stores explicit overrides.
CREATE TABLE notification_preferences (
    user_id    TEXT NOT NULL REFERENCES users(id),
    event_type TEXT NOT NULL,
    email      BOOLEAN NOT NULL DEFAULT TRUE,
    in_app     BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, event_type)
);

-- 3. Email verification. A user activates by consuming a token emailed on signup.
--    Existing users (pre-migration) are grandfathered in as already verified so
--    the new login gate doesn't lock anyone out.
ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;
UPDATE users SET email_verified_at = NOW() WHERE email_verified_at IS NULL;

CREATE TABLE email_verifications (
    id          TEXT PRIMARY KEY DEFAULT gen_ulid(),
    user_id     TEXT NOT NULL REFERENCES users(id),
    token       TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_email_verifications_token ON email_verifications (token) WHERE consumed_at IS NULL;
CREATE INDEX idx_email_verifications_user ON email_verifications (user_id);
