-- name: CreateUser :one
INSERT INTO users (company_id, email, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1;

-- name: ListUsersByCompany :many
SELECT * FROM users
WHERE company_id = $1
ORDER BY created_at;
