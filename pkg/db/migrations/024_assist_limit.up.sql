-- Buyer AI assist metering: Free is capped, paid plans unlimited (-1).
ALTER TABLE plans ADD COLUMN assist_monthly_limit INTEGER NOT NULL DEFAULT 20;
UPDATE plans SET assist_monthly_limit = -1 WHERE code IN ('pro', 'business');
-- Free (code='free') keeps the default 20.
