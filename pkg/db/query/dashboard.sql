-- name: UpsertRFQView :exec
-- Records that the buyer has now seen this RFQ's quotes (clears its badge).
INSERT INTO rfq_views (company_id, rfq_id, last_viewed_at)
VALUES (@company_id, @rfq_id, NOW())
ON CONFLICT (company_id, rfq_id) DO UPDATE SET last_viewed_at = NOW();

-- name: BuyerDashboardCounts :one
-- All four buyer headline counts in one round-trip, tenant-scoped.
SELECT
    (SELECT COUNT(*) FROM rfqs r
       WHERE r.buyer_company_id = @company_id AND r.status IN ('open','quoted'))::bigint AS active_rfqs,
    (SELECT COUNT(*) FROM quotes q JOIN rfqs r ON r.id = q.rfq_id
       WHERE r.buyer_company_id = @company_id)::bigint AS quotes_received,
    (SELECT COUNT(*) FROM quotes q JOIN rfqs r ON r.id = q.rfq_id
       LEFT JOIN rfq_views v ON v.rfq_id = r.id AND v.company_id = r.buyer_company_id
       WHERE r.buyer_company_id = @company_id
         AND q.created_at > COALESCE(v.last_viewed_at, 'epoch'::timestamptz))::bigint AS new_quotes,
    (SELECT COUNT(*) FROM orders o
       WHERE o.buyer_company_id = @company_id
         AND o.status IN ('created','confirmed','fulfilled'))::bigint AS orders_in_progress;

-- name: ListBuyerRecentRFQs :many
-- Recent RFQs with quote counts + how many quotes arrived since last viewed.
SELECT r.id, r.title, r.status, r.created_at,
    (SELECT COUNT(*) FROM quotes q WHERE q.rfq_id = r.id)::bigint AS quote_count,
    (SELECT COUNT(*) FROM quotes q
       WHERE q.rfq_id = r.id
         AND q.created_at > COALESCE(v.last_viewed_at, 'epoch'::timestamptz))::bigint AS new_quotes
FROM rfqs r
LEFT JOIN rfq_views v ON v.rfq_id = r.id AND v.company_id = r.buyer_company_id
WHERE r.buyer_company_id = @company_id
ORDER BY r.created_at DESC
LIMIT @lim;

-- name: SellerDashboardCounts :one
-- Seller operational counts, tenant-scoped. review_queue = matched OPEN RFQs the
-- seller has not yet quoted (the actionable subset of incoming_rfqs).
SELECT
    (SELECT COUNT(*) FROM rfq_matches m JOIN rfqs r ON r.id = m.rfq_id
       WHERE m.seller_company_id = @company_id AND r.status = 'open')::bigint AS incoming_rfqs,
    (SELECT COUNT(*) FROM rfq_matches m JOIN rfqs r ON r.id = m.rfq_id
       WHERE m.seller_company_id = @company_id AND r.status = 'open'
         AND NOT EXISTS (SELECT 1 FROM quotes q
                         WHERE q.rfq_id = m.rfq_id AND q.seller_company_id = @company_id))::bigint AS review_queue,
    (SELECT COUNT(*) FROM orders o
       WHERE o.seller_company_id = @company_id
         AND o.status IN ('created','confirmed','fulfilled'))::bigint AS active_orders;

-- name: GetAgentPresence :one
-- Latest heartbeat among VALID (active, non-expired) credentials + whether any
-- valid credential exists. online is decided in Go against a freshness window.
SELECT
    (SELECT MAX(ac.last_seen) FROM agent_credentials ac
       WHERE ac.company_id = @company_id AND ac.status = 'active'
         AND (ac.expires_at IS NULL OR ac.expires_at > NOW())) AS last_seen,
    EXISTS (SELECT 1 FROM agent_credentials ac
       WHERE ac.company_id = @company_id AND ac.status = 'active'
         AND (ac.expires_at IS NULL OR ac.expires_at > NOW())) AS has_valid;
