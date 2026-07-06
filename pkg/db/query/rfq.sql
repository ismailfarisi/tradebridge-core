-- name: CreateRFQ :one
INSERT INTO rfqs (buyer_company_id, created_by, title, raw_text)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetRFQ :one
SELECT * FROM rfqs WHERE id = $1;

-- name: ListRFQsByBuyer :many
SELECT * FROM rfqs
WHERE buyer_company_id = $1
ORDER BY created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: MarkRFQMatched :one
UPDATE rfqs
SET requirements = sqlc.arg(requirements)::jsonb,
    category     = sqlc.arg(category),
    match_count  = sqlc.arg(match_count),
    status       = 'open',
    updated_at   = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetRFQStatus :one
UPDATE rfqs
SET status = sqlc.arg(status), updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateRFQMatch :one
INSERT INTO rfq_matches (rfq_id, seller_company_id, score, reason)
VALUES ($1, $2, $3, $4)
ON CONFLICT (rfq_id, seller_company_id) DO UPDATE
SET score = EXCLUDED.score, reason = EXCLUDED.reason
RETURNING *;

-- name: ListMatchesByRFQ :many
SELECT m.*, c.name AS seller_name
FROM rfq_matches m
JOIN companies c ON c.id = m.seller_company_id
WHERE m.rfq_id = $1
ORDER BY m.score DESC, m.created_at;

-- name: MatchSuppliers :many
SELECT sp.company_id, c.name AS company_name, sp.categories, sp.description, sp.location
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
WHERE sp.company_id <> sqlc.arg(exclude_company)
  AND (
    EXISTS (
        SELECT 1 FROM unnest(sp.categories) cat
        WHERE cat ILIKE '%' || sqlc.arg(needle)::text || '%'
    )
    OR sp.description ILIKE '%' || sqlc.arg(needle)::text || '%'
  )
LIMIT sqlc.arg(lim);

-- name: CreateAgentEvent :one
INSERT INTO agent_events (company_id, type, payload)
VALUES ($1, $2, sqlc.arg(payload)::jsonb)
RETURNING *;
