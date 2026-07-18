-- name: CreateWaitlistSignup :one
INSERT INTO waitlist_signups (email, source)
VALUES ($1, $2)
ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
RETURNING *;

-- name: ListWaitlist :many
SELECT * FROM waitlist_signups
WHERE (sqlc.narg(status_filter)::text IS NULL
       OR (sqlc.narg(status_filter) = 'pending' AND invited_at IS NULL)
       OR (sqlc.narg(status_filter) = 'invited' AND invited_at IS NOT NULL))
ORDER BY created_at DESC
LIMIT sqlc.arg(lim);

-- name: ListPendingWaitlistEmails :many
SELECT id, email FROM waitlist_signups WHERE invited_at IS NULL ORDER BY created_at;

-- name: GetWaitlistByIDs :many
SELECT id, email FROM waitlist_signups WHERE id = ANY(sqlc.arg(ids)::text[]) AND invited_at IS NULL;

-- name: MarkWaitlistInvited :exec
UPDATE waitlist_signups SET invited_at = NOW() WHERE id = ANY(sqlc.arg(ids)::text[]);
