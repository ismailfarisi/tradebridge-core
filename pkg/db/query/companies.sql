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

-- name: SearchCompaniesForAdmin :many
SELECT id, name, type, status, created_at
FROM companies
WHERE (sqlc.narg(q)::text IS NULL
       OR name ILIKE '%' || sqlc.narg(q) || '%'
       OR id = sqlc.narg(q))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY created_at DESC
LIMIT sqlc.arg(lim);

-- name: AdminCompanyCounts :one
SELECT
  (SELECT COUNT(*) FROM rfqs   r WHERE r.buyer_company_id  = $1) AS rfqs,
  (SELECT COUNT(*) FROM quotes q WHERE q.seller_company_id = $1) AS quotes,
  (SELECT COUNT(*) FROM orders o WHERE o.buyer_company_id  = $1 OR o.seller_company_id = $1) AS orders;
