-- Cross-instance exclusion and kill switches for the negotiation engine.

-- name: TryLockNegotiation :one
-- Transaction-scoped advisory lock keyed on the RFQ: held until the guarding
-- transaction ends (commit, rollback or a dropped connection), so a crashed
-- instance can never leave an RFQ locked.
SELECT pg_try_advisory_xact_lock(hashtextextended('negotiation:' || @rfq_id::text, 0))::bool AS locked;

-- name: LockNegotiation :exec
-- Blocking variant for buyer-initiated actions (approve/decline/pause); the
-- caller bounds the wait with lock_timeout.
SELECT pg_advisory_xact_lock(hashtextextended('negotiation:' || @rfq_id::text, 0));

-- name: SetNegotiationRoundPending :one
UPDATE negotiation_rounds
SET delivery_status = 'pending', quote_message_id = @quote_message_id, agent_event_id = sqlc.narg(agent_event_id)
WHERE id = @id
RETURNING *;

-- name: MarkNegotiationRoundSent :exec
UPDATE negotiation_rounds
SET delivery_status = 'sent', delivered_at = NOW()
WHERE id = @id AND delivery_status = 'pending';

-- name: ListPendingNegotiationDeliveries :many
SELECT * FROM negotiation_rounds
WHERE strategy_id = @strategy_id AND delivery_status = 'pending'
ORDER BY created_at ASC;

-- name: GetQuoteMessage :one
SELECT * FROM quote_messages WHERE id = $1;

-- name: GetAgentEventByID :one
SELECT * FROM agent_events WHERE id = $1;

-- name: GetNegotiationPauseState :one
-- FOR SHARE on the platform row: a kill-switch UPDATE waits for in-flight
-- engine transactions that already read "not paused", and every engine
-- transaction that starts after the flip commits sees it.
SELECT p.paused AS platform_paused, p.reason AS platform_reason,
    COALESCE(c.paused, FALSE)::bool AS company_paused, COALESCE(c.reason, '')::text AS company_reason
FROM negotiation_platform_control p
LEFT JOIN company_negotiation_controls c ON c.company_id = @company_id
WHERE p.id = 1
FOR SHARE OF p;

-- name: GetPlatformNegotiationControl :one
SELECT * FROM negotiation_platform_control WHERE id = 1;

-- name: SetPlatformNegotiationControl :one
UPDATE negotiation_platform_control
SET paused = @paused, reason = @reason, updated_by = @updated_by, updated_at = NOW()
WHERE id = 1
RETURNING *;

-- name: GetCompanyNegotiationControl :one
SELECT * FROM company_negotiation_controls WHERE company_id = $1;

-- name: UpsertCompanyNegotiationControl :one
INSERT INTO company_negotiation_controls (company_id, paused, reason, updated_by, updated_at)
VALUES (@company_id, @paused, @reason, sqlc.narg(updated_by), NOW())
ON CONFLICT (company_id) DO UPDATE
SET paused = EXCLUDED.paused, reason = EXCLUDED.reason, updated_by = EXCLUDED.updated_by, updated_at = NOW()
RETURNING *;

-- name: InsertNegotiationKillSwitchLog :one
INSERT INTO negotiation_kill_switch_log (scope, company_id, paused, reason, actor)
VALUES (@scope, sqlc.narg(company_id), @paused, @reason, @actor)
RETURNING *;

-- name: ListNegotiationKillSwitchLog :many
SELECT l.*, COALESCE(c.name, '')::text AS company_name
FROM negotiation_kill_switch_log l
LEFT JOIN companies c ON c.id = l.company_id
WHERE (sqlc.narg(company_id)::text IS NULL OR l.company_id = sqlc.narg(company_id))
ORDER BY l.created_at DESC
LIMIT @lim;
