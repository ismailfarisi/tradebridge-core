-- name: CreateCatalogSummary :one
INSERT INTO catalog_summaries (company_id, name, category, summary)
VALUES ($1, $2, $3, $4)
RETURNING id, company_id, name, category, summary, created_at, updated_at;

-- name: GetCatalogSummary :one
SELECT id, company_id, name, category, summary, created_at, updated_at
FROM catalog_summaries
WHERE id = $1;

-- name: ListCatalogByCompany :many
SELECT id, company_id, name, category, summary, created_at, updated_at
FROM catalog_summaries
WHERE company_id = $1
ORDER BY created_at DESC;

-- name: UpdateCatalogSummary :one
UPDATE catalog_summaries
SET name = $2, category = $3, summary = $4, updated_at = NOW()
WHERE id = $1
RETURNING id, company_id, name, category, summary, created_at, updated_at;

-- name: DeleteCatalogSummary :exec
DELETE FROM catalog_summaries
WHERE id = $1;

-- name: SearchCatalog :many
SELECT cs.id, cs.company_id, cs.name, cs.category, cs.summary, cs.created_at, cs.updated_at,
       c.name AS company_name
FROM catalog_summaries cs
JOIN companies c ON c.id = cs.company_id
WHERE (
    sqlc.arg(query)::text = ''
    OR cs.name ILIKE '%' || sqlc.arg(query)::text || '%'
    OR cs.category ILIKE '%' || sqlc.arg(query)::text || '%'
    OR cs.summary ILIKE '%' || sqlc.arg(query)::text || '%'
)
ORDER BY cs.created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);
