DROP INDEX IF EXISTS idx_companies_suspended;
ALTER TABLE companies DROP COLUMN IF EXISTS status;
