-- Sprint 10: nullable list prices per plan, used only to compute MRR when set.
-- NULL means "no price configured" — analytics must never fabricate MRR from
-- a plan with a NULL price.
ALTER TABLE plans ADD COLUMN price_cents_month INTEGER;
ALTER TABLE plans ADD COLUMN price_cents_year INTEGER;
