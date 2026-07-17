-- Order lifecycle: constrain status to the state machine and record every
-- transition in an append-only timeline used by both dashboards and as audit.
ALTER TABLE orders
    ADD CONSTRAINT orders_status_check
    CHECK (status IN ('created','confirmed','fulfilled','completed','cancelled','disputed'));

CREATE TABLE order_events (
    id               TEXT PRIMARY KEY DEFAULT gen_ulid(),
    order_id         TEXT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    from_status      TEXT NOT NULL,
    to_status        TEXT NOT NULL,
    actor            TEXT NOT NULL,            -- 'buyer' | 'seller' | 'admin'
    actor_company_id TEXT REFERENCES companies(id), -- NULL for admin actor
    note             TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_order_events_order ON order_events (order_id, created_at);
