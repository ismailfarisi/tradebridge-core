-- RFQ engine: buyers post plain-text requests, Core extracts structured
-- requirements, matches sellers, and records a durable event per matched agent.

CREATE TABLE rfqs (
    id               TEXT PRIMARY KEY DEFAULT gen_ulid(),
    buyer_company_id TEXT NOT NULL REFERENCES companies(id),
    created_by       TEXT NOT NULL REFERENCES users(id),
    title            TEXT NOT NULL,
    raw_text         TEXT NOT NULL,
    -- Structured requirements extracted from raw_text (category, quantity,
    -- specs, location, deadline, keywords). See internal/rfq/requirements.go.
    requirements     JSONB NOT NULL DEFAULT '{}',
    category         TEXT NOT NULL DEFAULT '',
    -- draft -> open -> quoted -> closed (see internal/rfq state machine).
    status           TEXT NOT NULL DEFAULT 'draft',
    match_count      INT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_rfqs_buyer ON rfqs (buyer_company_id, created_at DESC);

CREATE TABLE rfq_matches (
    id                TEXT PRIMARY KEY DEFAULT gen_ulid(),
    rfq_id            TEXT NOT NULL REFERENCES rfqs(id) ON DELETE CASCADE,
    seller_company_id TEXT NOT NULL REFERENCES companies(id),
    score             DOUBLE PRECISION NOT NULL DEFAULT 0,
    reason            TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (rfq_id, seller_company_id)
);

CREATE INDEX idx_rfq_matches_seller ON rfq_matches (seller_company_id);

-- Durable, per-agent event log. Every event destined for a seller's agent is
-- written here BEFORE it is published to Redis (the write-always pattern), so
-- the Gateway (Sprint A3) can replay from `seq` on reconnect via Last-Event-ID.
CREATE TABLE agent_events (
    seq          BIGSERIAL PRIMARY KEY,
    id           TEXT NOT NULL UNIQUE DEFAULT gen_ulid(),
    company_id   TEXT NOT NULL REFERENCES companies(id),
    type         TEXT NOT NULL,
    payload      JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at TIMESTAMPTZ
);

CREATE INDEX idx_agent_events_company_seq ON agent_events (company_id, seq);
