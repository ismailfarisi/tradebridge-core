-- Sprint 10 — waitlist invite flow: track when a signup was invited to launch.
ALTER TABLE waitlist_signups ADD COLUMN invited_at TIMESTAMPTZ;
