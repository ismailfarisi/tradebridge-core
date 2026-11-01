-- name: UpsertNegotiationBaseline :one
-- Snapshot of a closed negotiation's savings baselines. Re-closing (e.g. the
-- buyer approves a different supplier) replaces the snapshot.
INSERT INTO negotiation_baselines (
    strategy_id, rfq_id, quote_id, currency, final_cents, opening_cents, market_cents,
    market_quote_count, historical_cents, historical_order_id, historical_note, anchored,
    anchor_pct, anchor_basis, verified_baseline_cents, verified_basis, reason, source
) VALUES (
    @strategy_id, @rfq_id, @quote_id, @currency, @final_cents, sqlc.narg(opening_cents), sqlc.narg(market_cents),
    @market_quote_count, sqlc.narg(historical_cents), sqlc.narg(historical_order_id), @historical_note, @anchored,
    sqlc.narg(anchor_pct), sqlc.narg(anchor_basis), @verified_baseline_cents, @verified_basis, @reason, @source
)
ON CONFLICT (strategy_id) DO UPDATE
SET quote_id = EXCLUDED.quote_id,
    currency = EXCLUDED.currency,
    final_cents = EXCLUDED.final_cents,
    opening_cents = EXCLUDED.opening_cents,
    market_cents = EXCLUDED.market_cents,
    market_quote_count = EXCLUDED.market_quote_count,
    historical_cents = EXCLUDED.historical_cents,
    historical_order_id = EXCLUDED.historical_order_id,
    historical_note = EXCLUDED.historical_note,
    anchored = EXCLUDED.anchored,
    anchor_pct = EXCLUDED.anchor_pct,
    anchor_basis = EXCLUDED.anchor_basis,
    verified_baseline_cents = EXCLUDED.verified_baseline_cents,
    verified_basis = EXCLUDED.verified_basis,
    reason = EXCLUDED.reason,
    source = EXCLUDED.source,
    computed_at = NOW()
RETURNING *;

-- name: GetNegotiationBaseline :one
SELECT * FROM negotiation_baselines WHERE strategy_id = $1;

-- name: ListQuoteOpenings :many
-- Every supplier's opening (v1) terms on an RFQ, with currency, for the
-- market baseline and anchoring checks.
SELECT qv.quote_id, qv.price_cents, qv.currency
FROM quote_versions qv JOIN quotes q ON q.id = qv.quote_id
WHERE q.rfq_id = $1 AND qv.version = 1
ORDER BY qv.quote_id;

-- name: ListBuyerPurchaseHistory :many
-- The buyer's own prior purchases in a category and currency, newest first,
-- for the historical baseline. TradeBridge has no payment record, so an order
-- the supplier has confirmed (or later fulfilled/completed) stands in for a
-- paid purchase; created, cancelled and disputed orders are excluded.
SELECT o.id AS order_id, o.rfq_id, o.total_cents, o.currency, o.created_at,
    r.title AS rfq_title, r.category, r.requirements
FROM orders o
JOIN rfqs r ON r.id = o.rfq_id
WHERE o.buyer_company_id = @buyer_company_id
  AND o.status IN ('confirmed', 'fulfilled', 'completed')
  AND o.currency = @currency
  AND lower(btrim(r.category)) = lower(btrim(@category::text))
  AND btrim(r.category) <> ''
  AND o.rfq_id <> @exclude_rfq_id
  AND o.created_at >= @since
ORDER BY o.created_at DESC
LIMIT 50;

-- name: BuyerDealBaselines :many
-- One row per buyer order with what the savings report needs to pick an
-- honest baseline: the snapshot taken at close when there is one, otherwise
-- every supplier's opening price on the RFQ in the order's currency.
SELECT o.id AS order_id, o.created_at, o.total_cents, o.currency, o.status, o.rfq_id,
    COALESCE((SELECT qv.price_cents FROM quote_versions qv
      WHERE qv.quote_id = o.quote_id AND qv.version = 1 AND qv.currency = o.currency), 0)::bigint AS opening_cents,
    ARRAY(SELECT qv.price_cents FROM quote_versions qv JOIN quotes q ON q.id = qv.quote_id
           WHERE q.rfq_id = o.rfq_id AND qv.version = 1 AND qv.currency = o.currency
           ORDER BY qv.price_cents)::bigint[] AS rfq_openings,
    (nb.strategy_id IS NOT NULL)::boolean AS has_snapshot,
    nb.market_cents, nb.historical_cents, nb.anchored, nb.anchor_pct, nb.anchor_basis,
    nb.verified_baseline_cents, nb.verified_basis, nb.reason, nb.source
FROM orders o
LEFT JOIN negotiation_baselines nb ON nb.quote_id = o.quote_id AND nb.currency = o.currency
WHERE o.buyer_company_id = @buyer_company_id
ORDER BY o.created_at DESC;

-- name: ListBuyerNegotiationBaselines :many
SELECT nb.* FROM negotiation_baselines nb
JOIN negotiation_strategies s ON s.id = nb.strategy_id
WHERE s.buyer_company_id = @buyer_company_id;
