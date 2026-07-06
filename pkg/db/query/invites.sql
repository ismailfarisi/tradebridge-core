-- name: CreateInvite :one
INSERT INTO user_invites (company_id, email, role, token, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetInviteByToken :one
SELECT * FROM user_invites
WHERE token = $1
  AND accepted_at IS NULL
  AND expires_at > NOW();

-- name: MarkInviteAccepted :exec
UPDATE user_invites SET accepted_at = NOW()
WHERE id = $1;

-- name: ListInvitesByCompany :many
SELECT * FROM user_invites
WHERE company_id = $1
ORDER BY created_at DESC;
