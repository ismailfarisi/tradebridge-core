-- name: CreateCompany :one
INSERT INTO companies (name, type)
VALUES ($1, $2)
RETURNING *;

-- name: GetCompanyByID :one
SELECT * FROM companies
WHERE id = $1;

-- name: GetCompanyStatus :one
SELECT status FROM companies WHERE id = $1;

-- name: SetCompanyStatus :exec
UPDATE companies SET status = $2 WHERE id = $1;
