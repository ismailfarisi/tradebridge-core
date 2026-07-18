-- name: CreateFeedback :one
INSERT INTO feedback (company_id, user_id, kind, score, message)
VALUES ($1, $2, $3, sqlc.narg(score), sqlc.narg(message))
RETURNING *;

-- name: ListFeedback :many
SELECT * FROM feedback ORDER BY created_at DESC LIMIT sqlc.arg(lim);
