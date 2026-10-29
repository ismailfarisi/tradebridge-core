-- Autonomous buyer-side negotiation. A strategy is the buyer's mandate for one
-- RFQ (target, walk-away, delivery need, trade-off weight, round budget); the
-- engine counters every supplier quote within that mandate and logs each move
-- as an immutable round. UNIQUE(quote_id, responded_to_version) makes the
-- engine idempotent: it answers each seller version at most once.
CREATE TABLE negotiation_strategies (
    id                 TEXT PRIMARY KEY DEFAULT gen_ulid(),
    rfq_id             TEXT NOT NULL UNIQUE REFERENCES rfqs(id) ON DELETE CASCADE,
    buyer_company_id   TEXT NOT NULL REFERENCES companies(id),
    created_by         TEXT REFERENCES users(id),
    target_price_cents BIGINT NOT NULL CHECK (target_price_cents > 0),
    max_price_cents    BIGINT NOT NULL,
    max_delivery_days  INT CHECK (max_delivery_days IS NULL OR max_delivery_days > 0),
    price_weight       DOUBLE PRECISION NOT NULL DEFAULT 0.7 CHECK (price_weight >= 0 AND price_weight <= 1),
    max_rounds         INT NOT NULL DEFAULT 4 CHECK (max_rounds BETWEEN 1 AND 10),
    auto_accept        BOOLEAN NOT NULL DEFAULT FALSE,
    tone               TEXT NOT NULL DEFAULT 'collaborative' CHECK (tone IN ('collaborative', 'firm')),
    status             TEXT NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'paused', 'awaiting_approval', 'completed', 'cancelled')),
    outcome_quote_id   TEXT REFERENCES quotes(id) ON DELETE SET NULL,
    outcome_summary    TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (max_price_cents >= target_price_cents)
);

CREATE INDEX idx_negotiation_strategies_buyer ON negotiation_strategies (buyer_company_id, created_at DESC);
CREATE INDEX idx_negotiation_strategies_active ON negotiation_strategies (updated_at) WHERE status = 'active';

CREATE TABLE negotiation_rounds (
    id                    TEXT PRIMARY KEY DEFAULT gen_ulid(),
    strategy_id           TEXT NOT NULL REFERENCES negotiation_strategies(id) ON DELETE CASCADE,
    quote_id              TEXT NOT NULL REFERENCES quotes(id) ON DELETE CASCADE,
    round                 INT NOT NULL,
    responded_to_version  INT NOT NULL,
    action                TEXT NOT NULL CHECK (action IN ('counter', 'accept_ready', 'walk_away')),
    seller_price_cents    BIGINT NOT NULL,
    seller_delivery_days  INT,
    offer_price_cents     BIGINT,
    offer_delivery_days   INT,
    utility               DOUBLE PRECISION NOT NULL DEFAULT 0,
    rationale             TEXT NOT NULL DEFAULT '',
    message               TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (quote_id, responded_to_version)
);

CREATE INDEX idx_negotiation_rounds_strategy ON negotiation_rounds (strategy_id, created_at);
