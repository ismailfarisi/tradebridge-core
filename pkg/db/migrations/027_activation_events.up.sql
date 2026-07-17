-- Sprint 7: activation funnel events. One row per (company, milestone) — the
-- UNIQUE makes Emit idempotent so funnels count first-reach, not repeats.
CREATE TABLE activation_events (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id TEXT NOT NULL REFERENCES companies(id),
    user_id    TEXT REFERENCES users(id),
    event      TEXT NOT NULL,
    props      JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (company_id, event)
);

CREATE INDEX idx_activation_events_funnel ON activation_events (event, created_at);
