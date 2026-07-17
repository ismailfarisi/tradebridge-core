-- name: UpsertRFQView :exec
-- Records that the buyer has now seen this RFQ's quotes (clears its badge).
INSERT INTO rfq_views (company_id, rfq_id, last_viewed_at)
VALUES (@company_id, @rfq_id, NOW())
ON CONFLICT (company_id, rfq_id) DO UPDATE SET last_viewed_at = NOW();
