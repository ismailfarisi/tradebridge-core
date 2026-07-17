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
