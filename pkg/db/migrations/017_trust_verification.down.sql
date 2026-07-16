DROP TABLE IF EXISTS domain_verifications;
DROP INDEX IF EXISTS idx_companies_verified_domain;
ALTER TABLE companies
    DROP COLUMN IF EXISTS verified_at,
    DROP COLUMN IF EXISTS verified_domain,
    DROP COLUMN IF EXISTS verification_status;
