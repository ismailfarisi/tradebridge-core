-- Persist the buyer↔assistant chat transcript that produced an RFQ. Nullable:
-- RFQs created via the classic /v1/rfqs form have no transcript.
ALTER TABLE rfqs ADD COLUMN messages JSONB;
