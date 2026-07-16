-- name: ListPlans :many
SELECT * FROM plans ORDER BY
    CASE code WHEN 'free' THEN 0 WHEN 'pro' THEN 1 WHEN 'business' THEN 2 ELSE 3 END;

-- name: GetPlan :one
SELECT * FROM plans WHERE code = $1;

-- name: UpdatePlanLimits :one
UPDATE plans
SET agent_autonomy    = $2,
    catalog_max_items = $3,
    rfq_monthly_limit = $4,
    seats             = $5,
    updated_at        = NOW()
WHERE code = $1
RETURNING *;

-- name: GetActiveSubscription :one
SELECT * FROM subscriptions
WHERE company_id = $1
  AND status IN ('trialing', 'active', 'past_due');

-- name: GetUsageCounter :one
SELECT count FROM usage_counters
WHERE company_id = $1 AND metric = $2 AND period = $3;

-- name: IncrementUsageCounter :one
INSERT INTO usage_counters (company_id, metric, period, count)
VALUES ($1, $2, $3, 1)
ON CONFLICT (company_id, metric, period) DO UPDATE
SET count = usage_counters.count + 1
RETURNING count;

-- name: CountCatalogByCompany :one
SELECT COUNT(*) FROM catalog_summaries WHERE company_id = $1;

-- name: CountUsersByCompany :one
SELECT COUNT(*) FROM users WHERE company_id = $1;

-- name: CountPendingInvitesByCompany :one
SELECT COUNT(*) FROM user_invites
WHERE company_id = $1 AND accepted_at IS NULL AND expires_at > NOW();
