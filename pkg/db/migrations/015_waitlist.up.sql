-- Sprint 1 — public waitlist capture (switeaz.com landing → S10 launch invite).
CREATE TABLE waitlist_signups (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    email      TEXT NOT NULL UNIQUE,
    source     TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
