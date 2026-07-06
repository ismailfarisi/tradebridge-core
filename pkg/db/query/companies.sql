-- name: CreateCompany :one
INSERT INTO companies (name, type)
VALUES ($1, $2)
RETURNING *;

-- name: GetCompanyByID :one
SELECT * FROM companies
WHERE id = $1;
