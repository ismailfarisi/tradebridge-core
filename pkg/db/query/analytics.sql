-- name: AnalyticsGMV :one
-- Cancelled literal ('cancelled', double-l) matches the orders_status_check
-- constraint (migration 019) and the existing seller-analytics revenue query
-- in pkg/db/query/dashboard.sql.
SELECT COALESCE(SUM(total_cents), 0)::bigint AS gmv_cents,
       COUNT(*)::bigint AS orders
FROM orders
WHERE status <> 'cancelled';

-- name: AnalyticsGMVByWeek :many
SELECT date_trunc('week', created_at)::date AS week,
       COALESCE(SUM(total_cents), 0)::bigint AS gmv_cents
FROM orders
WHERE status <> 'cancelled' AND created_at > NOW() - INTERVAL '12 weeks'
GROUP BY 1 ORDER BY 1;

-- name: AnalyticsActiveCompanies :one
SELECT
  (SELECT COUNT(DISTINCT buyer_company_id) FROM rfqs  WHERE created_at > NOW() - INTERVAL '30 days')::bigint AS active_buyers,
  (SELECT COUNT(DISTINCT seller_company_id) FROM quotes WHERE created_at > NOW() - INTERVAL '30 days')::bigint AS active_sellers;

-- name: AnalyticsFunnel :many
SELECT event, COUNT(DISTINCT company_id)::bigint AS companies
FROM activation_events GROUP BY event ORDER BY event;

-- name: AnalyticsSubscriptionMix :many
SELECT s.plan, s.status, COUNT(*)::bigint AS n,
       p.price_cents_month, p.price_cents_year
FROM subscriptions s JOIN plans p ON p.code = s.plan
GROUP BY s.plan, s.status, p.price_cents_month, p.price_cents_year;
