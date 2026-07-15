-- Buyer-uploaded RFQ attachments (images + PDFs). Bytes live on local disk
-- (internal/attachments.Store); this table is metadata + authorization scope.
-- rfq_id is stamped once the attachment is referenced by an RFQ or a quote
-- message, and gates seller/agent download access.

CREATE TABLE attachments (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id TEXT NOT NULL REFERENCES companies(id),
    rfq_id     TEXT REFERENCES rfqs(id) ON DELETE SET NULL,
    filename   TEXT NOT NULL,
    mime       TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    sha256     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_attachments_company ON attachments (company_id, created_at DESC);
CREATE INDEX idx_attachments_rfq ON attachments (rfq_id);

-- Attachment metadata on buyer/seller thread messages:
-- [{"id","filename","mime","size_bytes"}]
ALTER TABLE quote_messages ADD COLUMN attachments JSONB;
