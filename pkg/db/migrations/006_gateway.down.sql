DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS agent_commands;
DROP INDEX IF EXISTS idx_agent_events_unacked;
ALTER TABLE agent_events DROP COLUMN IF EXISTS acked_at;
