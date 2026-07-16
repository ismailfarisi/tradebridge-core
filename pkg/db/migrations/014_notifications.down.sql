DROP TABLE IF EXISTS email_verifications;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
DROP TABLE IF EXISTS notification_preferences;
DROP TABLE IF EXISTS notifications;
