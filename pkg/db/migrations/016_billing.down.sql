ALTER TABLE companies DROP COLUMN IF EXISTS stripe_customer_id;
DROP TABLE IF EXISTS processed_stripe_events;
DROP TABLE IF EXISTS usage_counters;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS plans;
