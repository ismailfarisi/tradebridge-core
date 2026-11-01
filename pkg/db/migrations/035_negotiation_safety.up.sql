-- Negotiation safety: crash-safe counter delivery (outbox state on rounds) and
-- platform/company kill switches for the autonomous negotiation engine.

-- Outbox state for the message a round sends to the seller.
--   none    — the round sends nothing (accept_ready / walk_away)
--   pending — the round and its counter message are committed, but the live
--             side effects (Redis thread publish, seller notification, agent
--             event broadcast) have not been confirmed; the next tick replays them
--   sent    — side effects done
ALTER TABLE negotiation_rounds
    ADD COLUMN delivery_status TEXT NOT NULL DEFAULT 'none'
        CHECK (delivery_status IN ('none', 'pending', 'sent')),
    ADD COLUMN quote_message_id TEXT REFERENCES quote_messages(id) ON DELETE SET NULL,
    ADD COLUMN agent_event_id TEXT,
    ADD COLUMN delivered_at TIMESTAMPTZ;

-- Counters recorded before this migration were delivered inline.
UPDATE negotiation_rounds SET delivery_status = 'sent', delivered_at = created_at WHERE action = 'counter';

CREATE INDEX idx_negotiation_rounds_pending ON negotiation_rounds (strategy_id) WHERE delivery_status = 'pending';

-- Platform-wide kill switch: exactly one row.
CREATE TABLE negotiation_platform_control (
    id         SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    paused     BOOLEAN NOT NULL DEFAULT FALSE,
    reason     TEXT NOT NULL DEFAULT '',
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO negotiation_platform_control (id) VALUES (1);

-- Per-company kill switch (owner/admin controlled). No row = not paused.
CREATE TABLE company_negotiation_controls (
    company_id TEXT PRIMARY KEY REFERENCES companies(id) ON DELETE CASCADE,
    paused     BOOLEAN NOT NULL DEFAULT FALSE,
    reason     TEXT NOT NULL DEFAULT '',
    updated_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Append-only history of every kill-switch flip (platform rows have no
-- company, so they cannot go to audit_logs, which is per-company).
CREATE TABLE negotiation_kill_switch_log (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    scope      TEXT NOT NULL CHECK (scope IN ('platform', 'company')),
    company_id TEXT REFERENCES companies(id) ON DELETE CASCADE,
    paused     BOOLEAN NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',
    actor      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((scope = 'platform') = (company_id IS NULL))
);

CREATE INDEX idx_negotiation_kill_switch_log_created ON negotiation_kill_switch_log (created_at DESC);
