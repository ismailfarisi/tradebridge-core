-- Every RFQ gets an expiry deadline; the sweeper (Task C4) closes overdue open
-- RFQs and notifies matched agents. Backfill existing open/quoted rows so they
-- are swept on a sane schedule rather than never.
ALTER TABLE rfqs ADD COLUMN expires_at TIMESTAMPTZ;

UPDATE rfqs
SET expires_at = created_at + INTERVAL '14 days'
WHERE status IN ('open', 'quoted') AND expires_at IS NULL;
