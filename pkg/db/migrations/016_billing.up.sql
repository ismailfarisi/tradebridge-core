-- Sprint 2 — Billing & Entitlements.
--
-- plans: the source of truth for per-plan entitlement values. Seeded with
-- defaults and editable by the platform admin (no deploy needed to tune limits).
-- A limit of -1 means unlimited.
CREATE TABLE plans (
    code              TEXT PRIMARY KEY CHECK (code IN ('free', 'pro', 'business')),
    name              TEXT NOT NULL,
    agent_autonomy    BOOLEAN NOT NULL,
    catalog_max_items INTEGER NOT NULL,
    rfq_monthly_limit INTEGER NOT NULL,
    seats             INTEGER NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO plans (code, name, agent_autonomy, catalog_max_items, rfq_monthly_limit, seats) VALUES
    ('free',     'Free',     FALSE, 25,  10,  2),
    ('pro',      'Pro',      TRUE,  500, 100, 5),
    ('business', 'Business', TRUE,  -1,  -1,  -1);

-- subscriptions: one row per company (its current plan). No row => Free.
-- Populated by Stripe webhooks in Plan 2; nothing writes here in Plan 1.
CREATE TABLE subscriptions (
    id                   TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id           TEXT NOT NULL UNIQUE REFERENCES companies(id),
    stripe_customer_id   TEXT,
    stripe_subscription_id TEXT,
    plan                 TEXT NOT NULL REFERENCES plans(code),
    interval             TEXT NOT NULL DEFAULT 'month' CHECK (interval IN ('month', 'year')),
    status               TEXT NOT NULL CHECK (status IN ('trialing','active','past_due','canceled','unpaid','incomplete')),
    current_period_end   TIMESTAMPTZ,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- usage_counters: incrementing counters for metered limits (currently rfq/month).
-- period is 'YYYY-MM' (UTC). Live counts (catalog, seats) are COUNT queries, not here.
CREATE TABLE usage_counters (
    company_id TEXT NOT NULL REFERENCES companies(id),
    metric     TEXT NOT NULL,
    period     TEXT NOT NULL,
    count      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (company_id, metric, period)
);

-- processed_stripe_events: webhook idempotency ledger (Plan 2 writes here).
CREATE TABLE processed_stripe_events (
    event_id     TEXT PRIMARY KEY,
    type         TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- companies gain a Stripe customer id (set on first checkout in Plan 2).
ALTER TABLE companies ADD COLUMN stripe_customer_id TEXT;
