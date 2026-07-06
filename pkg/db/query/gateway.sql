-- name: ReplayAgentEventsSince :many
-- Events after a Last-Event-ID cursor (the SSE resume path).
SELECT * FROM agent_events
WHERE company_id = $1 AND seq > sqlc.arg(after_seq)
ORDER BY seq
LIMIT sqlc.arg(lim);

-- name: ReplayUnackedAgentEvents :many
-- Fallback when a client reconnects without a Last-Event-ID: everything still
-- unacked for this company.
SELECT * FROM agent_events
WHERE company_id = $1 AND acked_at IS NULL
ORDER BY seq
LIMIT sqlc.arg(lim);

-- name: AckAgentEvent :one
UPDATE agent_events
SET acked_at = NOW()
WHERE seq = sqlc.arg(seq) AND company_id = $1
RETURNING seq;

-- name: CreateAgentCommand :one
-- Idempotent insert: a duplicate (agent_credential_id, idempotency_key) returns
-- no row, signalling the caller to fetch the original.
INSERT INTO agent_commands (agent_credential_id, company_id, rfq_id, type, idempotency_key, payload)
VALUES ($1, $2, sqlc.narg(rfq_id), $3, $4, sqlc.arg(payload)::jsonb)
ON CONFLICT (agent_credential_id, idempotency_key) DO NOTHING
RETURNING *;

-- name: GetAgentCommandByKey :one
SELECT * FROM agent_commands
WHERE agent_credential_id = $1 AND idempotency_key = $2;

-- name: CreateAuditLog :one
INSERT INTO audit_logs (company_id, actor, action, target_id, detail)
VALUES ($1, $2, $3, sqlc.narg(target_id), sqlc.arg(detail)::jsonb)
RETURNING *;

-- name: RFQMatchExists :one
SELECT EXISTS (
    SELECT 1 FROM rfq_matches
    WHERE rfq_id = $1 AND seller_company_id = $2
) AS matched;
