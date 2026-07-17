-- name: InsertActivationEvent :exec
-- Idempotent milestone record; the UNIQUE(company_id,event) makes a repeat a no-op.
INSERT INTO activation_events (company_id, user_id, event, props)
VALUES (@company_id, @user_id, @event, @props)
ON CONFLICT (company_id, event) DO NOTHING;

-- name: GetActivationSignals :one
-- All onboarding-step signals for a company in one round-trip. (verification_status
-- is read from the company row in Go — Service.Status already fetches it — so it is
-- intentionally NOT selected here, avoiding a nullable scalar-subquery type.)
SELECT
    EXISTS (SELECT 1 FROM supplier_profiles sp
            WHERE sp.company_id = @company_id
              AND COALESCE(sp.description, '') <> ''
              AND COALESCE(array_length(sp.categories, 1), 0) >= 1) AS has_profile,
    (SELECT COUNT(*) FROM catalog_summaries WHERE company_id = @company_id)::bigint AS catalog_count,
    EXISTS (SELECT 1 FROM agent_credentials
            WHERE company_id = @company_id AND status = 'active'
              AND (expires_at IS NULL OR expires_at > NOW())
              AND last_seen IS NOT NULL) AS agent_connected,
    (SELECT COUNT(*) FROM quotes WHERE seller_company_id = @company_id)::bigint AS quotes_submitted,
    (SELECT COUNT(*) FROM rfqs WHERE buyer_company_id = @company_id)::bigint AS rfqs_posted,
    (SELECT COUNT(*) FROM quotes q JOIN rfqs r ON r.id = q.rfq_id
       WHERE r.buyer_company_id = @company_id)::bigint AS quotes_received,
    (SELECT COUNT(*) FROM orders WHERE buyer_company_id = @company_id)::bigint AS orders_as_buyer;

-- name: CountActivationEventsFor :one
SELECT COUNT(*) FROM activation_events WHERE company_id = @company_id AND event = @event;
