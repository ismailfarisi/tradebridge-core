-- name: CreateUser :one
INSERT INTO users (company_id, email, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateUserWithPassword :one
INSERT INTO users (company_id, email, role, password_hash)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = $2
WHERE id = $1;

-- name: ListUsersByCompany :many
SELECT * FROM users
WHERE company_id = $1
ORDER BY created_at;
