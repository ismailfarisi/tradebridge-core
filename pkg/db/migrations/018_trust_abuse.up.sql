-- Sprint 3 — Trust & Safety I: abuse reports + blocks.
CREATE TABLE reports (
    id                  TEXT PRIMARY KEY DEFAULT gen_ulid(),
    reporter_company_id TEXT NOT NULL REFERENCES companies(id),
    reported_company_id TEXT NOT NULL REFERENCES companies(id),
    reason              TEXT NOT NULL
        CHECK (reason IN ('spam','fraud','abuse','inappropriate','other')),
    detail              TEXT,
    status              TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open','reviewed','dismissed','actioned')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at         TIMESTAMPTZ,
    CHECK (reporter_company_id <> reported_company_id)
);
CREATE INDEX idx_reports_status ON reports (status, created_at DESC);
CREATE INDEX idx_reports_reported ON reports (reported_company_id);

CREATE TABLE blocks (
    id                 TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id         TEXT NOT NULL REFERENCES companies(id),
    blocked_company_id TEXT NOT NULL REFERENCES companies(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (company_id, blocked_company_id),
    CHECK (company_id <> blocked_company_id)
);
CREATE INDEX idx_blocks_company ON blocks (company_id);
CREATE INDEX idx_blocks_blocked ON blocks (blocked_company_id);
