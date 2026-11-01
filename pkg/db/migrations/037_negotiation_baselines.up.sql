-- Honest savings baselines. Savings measured only against a supplier's own
-- opening quote reward inflated openings, so every closed negotiation gets a
-- snapshot of harder-to-game references next to the opening quote:
--   market     — the lower median of all suppliers' opening quotes on the RFQ
--                (same currency, at least 2 quotes);
--   historical — the buyer's most recent confirmed purchase of a comparable
--                item, normalised per unit and scaled to this RFQ's quantity;
--   verified   — the lowest applicable reference, which is the headline figure.
-- An opening quote flagged as an anchor (far above the market or historical
-- reference) is never used as a baseline. Rows are written when a negotiation
-- closes (see internal/negotiation/baseline.go) and never recomputed on read,
-- so reported figures do not drift as new data arrives.
CREATE TABLE negotiation_baselines (
    strategy_id             TEXT PRIMARY KEY REFERENCES negotiation_strategies(id) ON DELETE CASCADE,
    rfq_id                  TEXT NOT NULL REFERENCES rfqs(id) ON DELETE CASCADE,
    quote_id                TEXT NOT NULL REFERENCES quotes(id) ON DELETE CASCADE,
    currency                TEXT NOT NULL,
    final_cents             BIGINT NOT NULL,
    opening_cents           BIGINT,
    market_cents            BIGINT,
    market_quote_count      INT NOT NULL DEFAULT 0,
    historical_cents        BIGINT,
    historical_order_id     TEXT REFERENCES orders(id) ON DELETE SET NULL,
    historical_note         TEXT NOT NULL DEFAULT '',
    anchored                BOOLEAN NOT NULL DEFAULT FALSE,
    anchor_pct              DOUBLE PRECISION,
    anchor_basis            TEXT CHECK (anchor_basis IS NULL OR anchor_basis IN ('market', 'historical')),
    verified_baseline_cents BIGINT NOT NULL,
    verified_basis          TEXT NOT NULL CHECK (verified_basis IN ('market', 'historical', 'opening_quote', 'none')),
    reason                  TEXT NOT NULL DEFAULT '',
    -- close: computed by the engine at close; backfill: reconstructed by this
    -- migration from immutable quote versions (no historical baseline).
    source                  TEXT NOT NULL CHECK (source IN ('close', 'backfill')),
    computed_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_negotiation_baselines_quote ON negotiation_baselines (quote_id);

-- Backfill negotiations that already picked a deal. Opening quotes live in
-- the immutable quote_versions (version 1), so the opening and market
-- references are reproducible exactly; the historical reference needs the
-- item-matching logic in Go and is left NULL (marked by source = 'backfill').
-- percentile_disc(0.5) is the lower median, matching LowerMedian in Go.
-- Thresholds mirror AnchorMarketThreshold (0.25) in baseline.go.
WITH deal AS (
    SELECT s.id AS strategy_id, s.rfq_id, q.id AS quote_id, q.currency,
           COALESCE(o.total_cents, q.price_cents) AS final_cents,
           (SELECT qv.price_cents FROM quote_versions qv
             WHERE qv.quote_id = q.id AND qv.version = 1 AND qv.currency = q.currency) AS opening_cents
    FROM negotiation_strategies s
    JOIN quotes q ON q.id = s.outcome_quote_id
    LEFT JOIN orders o ON o.quote_id = q.id
    WHERE s.status IN ('completed', 'awaiting_approval')
), refs AS (
    SELECT d.*,
           (SELECT COUNT(*) FROM quote_versions qv JOIN quotes q2 ON q2.id = qv.quote_id
             WHERE q2.rfq_id = d.rfq_id AND qv.version = 1 AND qv.currency = d.currency)::int AS market_quote_count,
           (SELECT percentile_disc(0.5) WITHIN GROUP (ORDER BY qv.price_cents)
              FROM quote_versions qv JOIN quotes q2 ON q2.id = qv.quote_id
             WHERE q2.rfq_id = d.rfq_id AND qv.version = 1 AND qv.currency = d.currency) AS market_all,
           (SELECT percentile_disc(0.5) WITHIN GROUP (ORDER BY qv.price_cents)
              FROM quote_versions qv JOIN quotes q2 ON q2.id = qv.quote_id
             WHERE q2.rfq_id = d.rfq_id AND qv.version = 1 AND qv.currency = d.currency
               AND q2.id <> d.quote_id) AS market_others
    FROM deal d
), flagged AS (
    SELECT r.*,
           CASE WHEN r.market_quote_count >= 2 THEN r.market_all END AS market_cents,
           (r.opening_cents IS NOT NULL AND r.market_others IS NOT NULL
            AND r.opening_cents > r.market_others * 1.25) AS anchored
    FROM refs r
), chosen AS (
    SELECT f.*,
           CASE WHEN f.anchored THEN NULL ELSE f.opening_cents END AS usable_opening
    FROM flagged f
)
INSERT INTO negotiation_baselines (
    strategy_id, rfq_id, quote_id, currency, final_cents, opening_cents, market_cents,
    market_quote_count, anchored, anchor_pct, anchor_basis, verified_baseline_cents,
    verified_basis, reason, source
)
SELECT c.strategy_id, c.rfq_id, c.quote_id, c.currency, c.final_cents, c.opening_cents, c.market_cents,
       c.market_quote_count, c.anchored,
       CASE WHEN c.anchored THEN ROUND(((c.opening_cents::numeric / c.market_others) - 1) * 100, 1)::float8 END,
       CASE WHEN c.anchored THEN 'market' END,
       COALESCE(LEAST(c.market_cents, c.usable_opening), c.final_cents),
       CASE
           WHEN c.market_cents IS NOT NULL AND (c.usable_opening IS NULL OR c.market_cents <= c.usable_opening) THEN 'market'
           WHEN c.usable_opening IS NOT NULL THEN 'opening_quote'
           ELSE 'none'
       END,
       CASE
           WHEN c.market_cents IS NULL AND c.usable_opening IS NOT NULL
               THEN 'Recorded before baselines were tracked; only the opening quote is available as a reference.'
           WHEN c.market_cents IS NULL
               THEN 'Recorded before baselines were tracked; no reference price is available.'
           ELSE 'Recorded before baselines were tracked; measured against the lowest of the opening quote and the typical opening price across suppliers. Past purchases were not checked.'
       END,
       'backfill'
FROM chosen c
ON CONFLICT DO NOTHING;
