-- Agent credentials: a seller company issues a secret its agent uses to connect
-- to the Gateway. Only the hash is stored; the plaintext is shown once at issue.

CREATE TABLE agent_credentials (
    id          TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id  TEXT NOT NULL REFERENCES companies(id),
    label       TEXT NOT NULL DEFAULT 'default',
    secret_hash TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    last_seen   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_credentials_company ON agent_credentials (company_id);
