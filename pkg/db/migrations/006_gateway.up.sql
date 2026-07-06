-- Gateway protocol: per-agent event acks, validated agent commands with
-- idempotency, and an audit trail for every accepted command.

-- Ack tracking on the durable event log. Replay on reconnect without a
-- Last-Event-ID falls back to "everything still unacked for this company".
ALTER TABLE agent_events ADD COLUMN acked_at TIMESTAMPTZ;

CREATE INDEX idx_agent_events_unacked
    ON agent_events (company_id, seq) WHERE acked_at IS NULL;

-- Commands an agent sends back to Core (quote.submit, clarification.request,
-- no_bid). The unique (agent_credential_id, idempotency_key) makes retries safe.
CREATE TABLE agent_commands (
    id                  TEXT PRIMARY KEY DEFAULT gen_ulid(),
    agent_credential_id TEXT NOT NULL REFERENCES agent_credentials(id),
    company_id          TEXT NOT NULL REFERENCES companies(id),
    rfq_id              TEXT REFERENCES rfqs(id),
    type                TEXT NOT NULL,
    idempotency_key     TEXT NOT NULL,
    payload             JSONB NOT NULL DEFAULT '{}',
    status              TEXT NOT NULL DEFAULT 'accepted',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (agent_credential_id, idempotency_key)
);

CREATE INDEX idx_agent_commands_rfq ON agent_commands (rfq_id);

-- Append-only audit trail. Every validated command writes one row.
CREATE TABLE audit_logs (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id TEXT NOT NULL REFERENCES companies(id),
    actor      TEXT NOT NULL,
    action     TEXT NOT NULL,
    target_id  TEXT,
    detail     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_company ON audit_logs (company_id, created_at DESC);
