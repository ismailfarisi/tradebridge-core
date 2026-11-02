-- name: UpsertNegotiationStrategy :one
INSERT INTO negotiation_strategies (
    rfq_id, buyer_company_id, created_by, target_price_cents, max_price_cents,
    max_delivery_days, price_weight, max_rounds, auto_accept, tone
) VALUES (
    @rfq_id, @buyer_company_id, sqlc.narg(created_by), @target_price_cents, @max_price_cents,
    sqlc.narg(max_delivery_days), @price_weight, @max_rounds, @auto_accept, @tone
)
ON CONFLICT (rfq_id) DO UPDATE
SET target_price_cents = EXCLUDED.target_price_cents,
    max_price_cents    = EXCLUDED.max_price_cents,
    max_delivery_days  = EXCLUDED.max_delivery_days,
    price_weight       = EXCLUDED.price_weight,
    max_rounds         = EXCLUDED.max_rounds,
    auto_accept        = EXCLUDED.auto_accept,
    tone               = EXCLUDED.tone,
    status             = CASE WHEN negotiation_strategies.status IN ('completed', 'cancelled')
                              THEN negotiation_strategies.status ELSE 'active' END,
    updated_at         = NOW()
RETURNING *;

-- name: GetNegotiationStrategyByRFQ :one
SELECT * FROM negotiation_strategies WHERE rfq_id = $1;

-- name: SetNegotiationStatus :one
UPDATE negotiation_strategies
SET status = @status,
    outcome_quote_id = COALESCE(sqlc.narg(outcome_quote_id), outcome_quote_id),
    outcome_summary = COALESCE(sqlc.narg(outcome_summary), outcome_summary),
    updated_at = NOW()
WHERE id = @id
RETURNING *;

-- name: ListActiveNegotiationRFQIDs :many
SELECT rfq_id FROM negotiation_strategies
WHERE status = 'active'
ORDER BY updated_at ASC
LIMIT @lim;

-- name: ListNegotiationsByBuyer :many
SELECT s.*, r.title AS rfq_title, r.status AS rfq_status,
    (SELECT COUNT(*) FROM quotes q WHERE q.rfq_id = s.rfq_id)::bigint AS quote_count,
    (SELECT COUNT(*) FROM negotiation_rounds nr WHERE nr.strategy_id = s.id AND nr.action = 'counter')::bigint AS round_count,
    COALESCE((SELECT MIN(qv.price_cents) FROM quote_versions qv JOIN quotes q ON q.id = qv.quote_id
              WHERE q.rfq_id = s.rfq_id AND qv.version = 1), 0)::bigint AS initial_best_cents,
    COALESCE((SELECT MIN(q.price_cents) FROM quotes q
              WHERE q.rfq_id = s.rfq_id AND q.status IN ('submitted', 'accepted')), 0)::bigint AS current_best_cents
FROM negotiation_strategies s
JOIN rfqs r ON r.id = s.rfq_id
WHERE s.buyer_company_id = @buyer_company_id
ORDER BY s.updated_at DESC
LIMIT @lim;

-- name: InsertNegotiationRound :one
-- ON CONFLICT DO NOTHING: a concurrent tick that already answered this seller
-- version wins; the caller sees pgx.ErrNoRows and skips.
INSERT INTO negotiation_rounds (
    strategy_id, quote_id, round, responded_to_version, action, seller_price_cents,
    seller_delivery_days, offer_price_cents, offer_delivery_days, utility, rationale, message
) VALUES (
    @strategy_id, @quote_id, @round, @responded_to_version, @action, @seller_price_cents,
    sqlc.narg(seller_delivery_days), sqlc.narg(offer_price_cents), sqlc.narg(offer_delivery_days),
    @utility, @rationale, @message
)
ON CONFLICT (quote_id, responded_to_version) DO NOTHING
RETURNING *;

-- name: DeleteNegotiationRound :exec
DELETE FROM negotiation_rounds WHERE id = $1;

-- name: ListNegotiationRounds :many
SELECT * FROM negotiation_rounds WHERE strategy_id = $1 ORDER BY created_at ASC, round ASC;

-- name: ListQuoteFirstVersions :many
SELECT qv.quote_id, qv.price_cents, qv.delivery_days
FROM quote_versions qv JOIN quotes q ON q.id = qv.quote_id
WHERE q.rfq_id = $1 AND qv.version = 1;

-- name: BuyerSavingsTotals :one
-- Spend and savings across the buyer's orders. Baseline = the cheapest
-- opening (v1) quote any supplier gave on the RFQ, matching the negotiation
-- room and list; savings = baseline - final order total, floored at 0.
SELECT
    COUNT(*)::bigint AS deals,
    COALESCE(SUM(o.total_cents), 0)::bigint AS spend_cents,
    COALESCE(SUM(GREATEST(COALESCE(qv.price_cents, o.total_cents) - o.total_cents, 0)), 0)::bigint AS savings_cents,
    COALESCE(SUM(COALESCE(qv.price_cents, o.total_cents)), 0)::bigint AS baseline_cents,
    COUNT(*) FILTER (WHERE s.id IS NOT NULL)::bigint AS negotiated_deals
FROM orders o
LEFT JOIN LATERAL (SELECT MIN(v1.price_cents) AS price_cents FROM quote_versions v1 JOIN quotes q1 ON q1.id = v1.quote_id
                   WHERE q1.rfq_id = o.rfq_id AND v1.version = 1) qv ON true
LEFT JOIN negotiation_strategies s ON s.rfq_id = o.rfq_id
WHERE o.buyer_company_id = @buyer_company_id AND o.status <> 'cancelled';

-- name: BuyerNegotiationCounts :one
SELECT
    COUNT(*) FILTER (WHERE status = 'active')::bigint AS active,
    COUNT(*) FILTER (WHERE status = 'awaiting_approval')::bigint AS awaiting_approval,
    COUNT(*) FILTER (WHERE status = 'completed')::bigint AS completed,
    COALESCE((SELECT AVG(c)::float8 FROM (
        SELECT COUNT(*) AS c FROM negotiation_rounds nr
        JOIN negotiation_strategies s2 ON s2.id = nr.strategy_id
        WHERE s2.buyer_company_id = @buyer_company_id AND nr.action = 'counter'
        GROUP BY nr.quote_id) x), 0)::float8 AS avg_rounds
FROM negotiation_strategies
WHERE buyer_company_id = @buyer_company_id;

-- name: BuyerMonthlySpend :many
SELECT to_char(date_trunc('month', o.created_at), 'YYYY-MM') AS month,
    COALESCE(SUM(o.total_cents), 0)::bigint AS spend_cents,
    COALESCE(SUM(GREATEST(COALESCE(qv.price_cents, o.total_cents) - o.total_cents, 0)), 0)::bigint AS savings_cents
FROM orders o
LEFT JOIN LATERAL (SELECT MIN(v1.price_cents) AS price_cents FROM quote_versions v1 JOIN quotes q1 ON q1.id = v1.quote_id
                   WHERE q1.rfq_id = o.rfq_id AND v1.version = 1) qv ON true
WHERE o.buyer_company_id = @buyer_company_id AND o.status <> 'cancelled'
  AND o.created_at >= date_trunc('month', NOW()) - INTERVAL '11 months'
GROUP BY 1
ORDER BY 1;

-- name: BuyerTopSuppliers :many
SELECT c.id, c.name, COUNT(*)::bigint AS deals, COALESCE(SUM(o.total_cents), 0)::bigint AS spend_cents
FROM orders o JOIN companies c ON c.id = o.seller_company_id
WHERE o.buyer_company_id = @buyer_company_id AND o.status <> 'cancelled'
GROUP BY c.id, c.name
ORDER BY spend_cents DESC
LIMIT 5;

-- name: BuyerRecentDeals :many
SELECT o.id AS order_id, o.created_at, o.total_cents, o.currency, r.id AS rfq_id, r.title AS rfq_title,
    c.name AS seller_name, COALESCE(qv.price_cents, o.total_cents)::bigint AS initial_cents,
    -- Counters the agent sent across every supplier on the RFQ, matching
    -- round_count in ListNegotiationsByBuyer.
    (SELECT COUNT(*) FROM negotiation_rounds nr JOIN negotiation_strategies ns ON ns.id = nr.strategy_id
     WHERE ns.rfq_id = o.rfq_id AND nr.action = 'counter')::bigint AS rounds
FROM orders o
JOIN rfqs r ON r.id = o.rfq_id
JOIN companies c ON c.id = o.seller_company_id
LEFT JOIN LATERAL (SELECT MIN(v1.price_cents) AS price_cents FROM quote_versions v1 JOIN quotes q1 ON q1.id = v1.quote_id
                   WHERE q1.rfq_id = o.rfq_id AND v1.version = 1) qv ON true
WHERE o.buyer_company_id = @buyer_company_id
ORDER BY o.created_at DESC
LIMIT 8;
