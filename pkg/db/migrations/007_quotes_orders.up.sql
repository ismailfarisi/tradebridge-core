-- Quotes + orders: normalize the agent's quote.submit into a first-class quote,
-- rank quotes for the buyer, and on acceptance auto-create a purchase order.

CREATE TABLE quotes (
    id                TEXT PRIMARY KEY DEFAULT gen_ulid(),
    rfq_id            TEXT NOT NULL REFERENCES rfqs(id) ON DELETE CASCADE,
    seller_company_id TEXT NOT NULL REFERENCES companies(id),
    agent_command_id  TEXT REFERENCES agent_commands(id),
    price_cents       BIGINT NOT NULL,
    currency          TEXT NOT NULL DEFAULT 'USD',
    delivery_days     INT,
    notes             TEXT NOT NULL DEFAULT '',
    -- 0..1 fraction of {price, delivery_days, notes} the seller supplied.
    completeness      DOUBLE PRECISION NOT NULL DEFAULT 0,
    status            TEXT NOT NULL DEFAULT 'submitted', -- submitted | accepted | rejected
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- One live quote per seller per RFQ; a resubmit updates it.
    UNIQUE (rfq_id, seller_company_id)
);

CREATE INDEX idx_quotes_rfq ON quotes (rfq_id);

CREATE TABLE quote_messages (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    quote_id   TEXT NOT NULL REFERENCES quotes(id) ON DELETE CASCADE,
    author     TEXT NOT NULL, -- 'buyer' | 'seller'
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_quote_messages_quote ON quote_messages (quote_id, created_at);

CREATE TABLE orders (
    id                TEXT PRIMARY KEY DEFAULT gen_ulid(),
    rfq_id            TEXT NOT NULL REFERENCES rfqs(id),
    quote_id          TEXT NOT NULL UNIQUE REFERENCES quotes(id),
    buyer_company_id  TEXT NOT NULL REFERENCES companies(id),
    seller_company_id TEXT NOT NULL REFERENCES companies(id),
    total_cents       BIGINT NOT NULL,
    currency          TEXT NOT NULL DEFAULT 'USD',
    status            TEXT NOT NULL DEFAULT 'created',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_orders_buyer ON orders (buyer_company_id, created_at DESC);
CREATE INDEX idx_orders_seller ON orders (seller_company_id, created_at DESC);
