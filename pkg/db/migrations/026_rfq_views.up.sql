-- Sprint 6: per-buyer last-seen marker per RFQ, backing the "new quotes since
-- you last looked" badge. One row per (company, rfq); upserted on RFQ view.
CREATE TABLE rfq_views (
    company_id     TEXT NOT NULL REFERENCES companies(id),
    rfq_id         TEXT NOT NULL REFERENCES rfqs(id) ON DELETE CASCADE,
    last_viewed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (company_id, rfq_id)
);
