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

-- name: UpsertSubscription :one
INSERT INTO subscriptions (
    company_id, stripe_customer_id, stripe_subscription_id, plan, interval,
    status, current_period_end, cancel_at_period_end, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
ON CONFLICT (company_id) DO UPDATE SET
    stripe_customer_id     = EXCLUDED.stripe_customer_id,
    stripe_subscription_id = EXCLUDED.stripe_subscription_id,
    plan                   = EXCLUDED.plan,
    interval               = EXCLUDED.interval,
    status                 = EXCLUDED.status,
    current_period_end     = EXCLUDED.current_period_end,
    cancel_at_period_end   = EXCLUDED.cancel_at_period_end,
    updated_at             = NOW()
RETURNING *;

-- name: GetSubscriptionByCompany :one
SELECT * FROM subscriptions WHERE company_id = $1;

-- name: SetCompanyStripeCustomer :exec
UPDATE companies SET stripe_customer_id = $2 WHERE id = $1;

-- name: GetCompanyByStripeCustomer :one
SELECT * FROM companies WHERE stripe_customer_id = $1;

-- name: IsStripeEventProcessed :one
SELECT EXISTS(SELECT 1 FROM processed_stripe_events WHERE event_id = $1);

-- name: MarkStripeEventProcessed :exec
INSERT INTO processed_stripe_events (event_id, type)
VALUES ($1, $2)
ON CONFLICT (event_id) DO NOTHING;
