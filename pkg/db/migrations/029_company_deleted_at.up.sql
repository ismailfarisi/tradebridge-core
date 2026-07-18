ALTER TABLE companies ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX idx_companies_deleted ON companies (deleted_at) WHERE deleted_at IS NOT NULL;
