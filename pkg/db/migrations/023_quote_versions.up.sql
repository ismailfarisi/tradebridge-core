-- Quote versioning: `quotes` remains the mutable head (ranking/accept/invoice
-- read it); quote_versions is the immutable append-only history. version 1 is
-- the seller's initial submit; each counter appends version N+1.
ALTER TABLE quotes ADD COLUMN version INT NOT NULL DEFAULT 1;

CREATE TABLE quote_versions (
    id               TEXT PRIMARY KEY DEFAULT gen_ulid(),
    quote_id         TEXT NOT NULL REFERENCES quotes(id) ON DELETE CASCADE,
    version          INT  NOT NULL,
    price_cents      BIGINT NOT NULL,
    currency         TEXT NOT NULL DEFAULT 'USD',
    delivery_days    INT,
    notes            TEXT NOT NULL DEFAULT '',
    completeness     DOUBLE PRECISION NOT NULL DEFAULT 0,
    source           TEXT NOT NULL CHECK (source IN ('seller','counter')),
    agent_command_id TEXT REFERENCES agent_commands(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (quote_id, version)
);

CREATE INDEX idx_quote_versions_quote ON quote_versions (quote_id, version);
