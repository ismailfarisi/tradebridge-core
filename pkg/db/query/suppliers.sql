-- name: UpsertSupplierProfile :one
INSERT INTO supplier_profiles (company_id, description, categories, location)
VALUES ($1, $2, $3, $4)
ON CONFLICT (company_id) DO UPDATE
SET description = EXCLUDED.description,
    categories  = EXCLUDED.categories,
    location    = EXCLUDED.location,
    updated_at  = NOW()
RETURNING *;

-- name: GetSupplierProfileByCompany :one
SELECT * FROM supplier_profiles
WHERE company_id = $1;

-- name: SearchSuppliers :many
SELECT sp.*, c.name AS company_name
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
WHERE (
    sqlc.arg(query)::text = ''
    OR c.name ILIKE '%' || sqlc.arg(query)::text || '%'
    OR sp.description ILIKE '%' || sqlc.arg(query)::text || '%'
    OR sp.location ILIKE '%' || sqlc.arg(query)::text || '%'
    OR EXISTS (
        SELECT 1 FROM unnest(sp.categories) cat
        WHERE cat ILIKE '%' || sqlc.arg(query)::text || '%'
    )
)
ORDER BY sp.verified DESC, c.name
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: GetPublicSeller :one
-- Tenant-safe storefront projection: explicit public columns only (no SELECT *
-- of companies, which would leak stripe_customer_id etc.).
SELECT c.id AS company_id,
       c.name,
       c.verification_status,
       c.verified_domain,
       sp.description,
       sp.categories,
       sp.location
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
WHERE sp.company_id = @company_id;
