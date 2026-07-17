-- Invoicing: one invoice per order, generated at quote acceptance. invoice_number
-- is human-readable and globally unique, drawn from a sequence: INV-<YYYY>-<000001>.
CREATE SEQUENCE IF NOT EXISTS invoice_number_seq;

CREATE TABLE invoices (
    id                TEXT PRIMARY KEY DEFAULT gen_ulid(),
    order_id          TEXT NOT NULL UNIQUE REFERENCES orders(id) ON DELETE CASCADE,
    invoice_number    TEXT NOT NULL UNIQUE,
    buyer_company_id  TEXT NOT NULL REFERENCES companies(id),
    seller_company_id TEXT NOT NULL REFERENCES companies(id),
    currency          TEXT NOT NULL DEFAULT 'USD',
    subtotal_cents    BIGINT NOT NULL,
    total_cents       BIGINT NOT NULL,
    issued_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE invoice_line_items (
    id               TEXT PRIMARY KEY DEFAULT gen_ulid(),
    invoice_id       TEXT NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    description      TEXT NOT NULL,
    quantity         BIGINT NOT NULL DEFAULT 1,
    unit_price_cents BIGINT NOT NULL,
    amount_cents     BIGINT NOT NULL
);

CREATE INDEX idx_invoice_line_items_invoice ON invoice_line_items (invoice_id);
