-- Company lifecycle status. 'suspended' companies cannot log in and are blocked
-- on every authed request by RequireActiveCompany (admin suspend action).
ALTER TABLE companies
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active'
    CHECK (status IN ('active', 'suspended'));

CREATE INDEX idx_companies_suspended ON companies (status) WHERE status = 'suspended';
