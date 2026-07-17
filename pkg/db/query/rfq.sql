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

-- name: ListRFQsByBuyerFiltered :many
-- Buyer RFQ list with tab filter (open|quoted|closed), text search, and per-RFQ
-- quote_count + new_quotes (quotes since the buyer last viewed the RFQ).
SELECT r.id, r.title, r.raw_text, r.requirements, r.category, r.status, r.match_count, r.created_at,
    (SELECT COUNT(*) FROM quotes q WHERE q.rfq_id = r.id)::bigint AS quote_count,
    (SELECT COUNT(*) FROM quotes q
       WHERE q.rfq_id = r.id
         AND q.created_at > COALESCE(v.last_viewed_at, 'epoch'::timestamptz))::bigint AS new_quotes
FROM rfqs r
LEFT JOIN rfq_views v ON v.rfq_id = r.id AND v.company_id = r.buyer_company_id
WHERE r.buyer_company_id = @company_id
  AND (@tab::text = ''
       OR (@tab::text = 'open'   AND r.status = 'open')
       OR (@tab::text = 'quoted' AND r.status = 'quoted')
       OR (@tab::text = 'closed' AND r.status IN ('closed','expired')))
  AND (@q::text = '' OR r.title ILIKE '%' || @q::text || '%' OR r.raw_text ILIKE '%' || @q::text || '%')
ORDER BY r.created_at DESC
LIMIT @lim OFFSET @off;

-- name: MarkRFQMatched :one
UPDATE rfqs
SET requirements = sqlc.arg(requirements)::jsonb,
    category     = sqlc.arg(category),
    match_count  = sqlc.arg(match_count),
    status       = 'open',
    expires_at   = sqlc.arg(expires_at),
    updated_at   = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetRFQStatus :one
UPDATE rfqs
SET status = sqlc.arg(status), updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetRFQMessages :exec
UPDATE rfqs
SET messages = sqlc.arg(messages)::jsonb, updated_at = NOW()
WHERE id = sqlc.arg(id);

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
  AND (NOT sqlc.arg(verified_only)::boolean OR c.verification_status = 'verified')
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

-- name: ListExpirableRFQIDs :many
SELECT id FROM rfqs
WHERE status IN ('open', 'quoted') AND expires_at IS NOT NULL AND expires_at <= NOW()
ORDER BY expires_at
LIMIT sqlc.arg(lim);
