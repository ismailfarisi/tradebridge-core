-- name: CreateEmailVerification :one
INSERT INTO email_verifications (user_id, token, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetEmailVerificationByToken :one
SELECT * FROM email_verifications
WHERE token = $1;

-- name: ConsumeEmailVerification :exec
UPDATE email_verifications SET consumed_at = NOW()
WHERE id = $1 AND consumed_at IS NULL;

-- name: MarkUserEmailVerified :exec
UPDATE users SET email_verified_at = NOW()
WHERE id = $1 AND email_verified_at IS NULL;
