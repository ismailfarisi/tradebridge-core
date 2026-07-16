-- Sprint 3 — Trust & Safety I: DNS-TXT domain verification.
-- Verification state lives on the company (both-role: one identity gates the
-- selling side's badge and match eligibility). domain_verifications holds the
-- active ownership challenge.

ALTER TABLE companies
    ADD COLUMN verification_status TEXT NOT NULL DEFAULT 'unverified'
        CHECK (verification_status IN ('unverified','pending','verified','rejected')),
    ADD COLUMN verified_domain TEXT,
    ADD COLUMN verified_at     TIMESTAMPTZ;

-- A domain can be *verified* by at most one company at a time. Pending claims
-- for the same domain by different companies are allowed (only one will win the
-- race to verified).
CREATE UNIQUE INDEX idx_companies_verified_domain
    ON companies (verified_domain) WHERE verification_status = 'verified';

CREATE TABLE domain_verifications (
    id              TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id      TEXT NOT NULL REFERENCES companies(id),
    domain          TEXT NOT NULL,
    token           TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','verified','failed')),
    attempts        INT  NOT NULL DEFAULT 0,
    last_checked_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    verified_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_domain_verifications_company_domain
    ON domain_verifications (company_id, domain);
CREATE INDEX idx_domain_verifications_company
    ON domain_verifications (company_id, created_at DESC);
