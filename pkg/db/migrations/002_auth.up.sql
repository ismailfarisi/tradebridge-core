-- Auth: passwords on users + an invite flow for adding teammates to a company.

ALTER TABLE users ADD COLUMN password_hash TEXT;

CREATE TABLE user_invites (
    id          TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id  TEXT NOT NULL REFERENCES companies(id),
    email       TEXT NOT NULL,
    role        TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    token       TEXT NOT NULL UNIQUE,
    invited_by  TEXT NOT NULL REFERENCES users(id),
    expires_at  TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_invites_company ON user_invites (company_id);
CREATE INDEX idx_user_invites_token ON user_invites (token) WHERE accepted_at IS NULL;
