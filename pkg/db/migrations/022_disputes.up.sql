-- Disputes: a party escalates an active order; an admin resolves it. At most one
-- OPEN dispute per order (partial-unique). Resolution moves the order terminal.
CREATE TABLE disputes (
    id                   TEXT PRIMARY KEY DEFAULT gen_ulid(),
    order_id             TEXT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    raised_by_company_id TEXT NOT NULL REFERENCES companies(id),
    reason               TEXT NOT NULL CHECK (reason IN ('not_delivered','not_as_described','payment','other')),
    detail               TEXT NOT NULL DEFAULT '',
    status               TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','rejected')),
    resolution           TEXT NOT NULL DEFAULT '',
    resolved_by_admin_id TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at          TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_disputes_one_open ON disputes (order_id) WHERE status = 'open';
CREATE INDEX idx_disputes_status ON disputes (status, created_at DESC);
