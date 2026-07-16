-- name: CreateWaitlistSignup :one
INSERT INTO waitlist_signups (email, source)
VALUES ($1, $2)
ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
RETURNING *;
