DROP TABLE IF EXISTS user_invites;
ALTER TABLE users DROP COLUMN IF EXISTS password_hash;
