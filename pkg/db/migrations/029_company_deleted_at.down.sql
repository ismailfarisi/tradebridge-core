DROP INDEX IF EXISTS idx_companies_deleted;
ALTER TABLE companies DROP COLUMN IF EXISTS deleted_at;
