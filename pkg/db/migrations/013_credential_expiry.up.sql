-- Agent credentials gain an optional expiry (NULL = never). Grandfather existing
-- ACTIVE credentials onto the policy with a full 90-day runway from deploy so the
-- live agent keeps working (migrations are static SQL and cannot read the env TTL).
ALTER TABLE agent_credentials ADD COLUMN expires_at TIMESTAMPTZ;

UPDATE agent_credentials
   SET expires_at = NOW() + INTERVAL '90 days'
 WHERE expires_at IS NULL AND status = 'active';
