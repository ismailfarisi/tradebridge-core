DROP TABLE IF EXISTS negotiation_kill_switch_log;
DROP TABLE IF EXISTS company_negotiation_controls;
DROP TABLE IF EXISTS negotiation_platform_control;
DROP INDEX IF EXISTS idx_negotiation_rounds_pending;
ALTER TABLE negotiation_rounds
    DROP COLUMN IF EXISTS delivered_at,
    DROP COLUMN IF EXISTS agent_event_id,
    DROP COLUMN IF EXISTS quote_message_id,
    DROP COLUMN IF EXISTS delivery_status;
