-- name: UpsertSupplierProfile :one
INSERT INTO supplier_profiles (company_id, description, categories, location)
VALUES ($1, $2, $3, $4)
ON CONFLICT (company_id) DO UPDATE
SET description = EXCLUDED.description,
    categories  = EXCLUDED.categories,
    location    = EXCLUDED.location,
    updated_at  = NOW()
RETURNING id, company_id, description, categories, location, verified, created_at, updated_at;

-- name: GetSupplierProfileByCompany :one
SELECT id, company_id, description, categories, location, verified, created_at, updated_at
FROM supplier_profiles
WHERE company_id = $1;

-- name: SearchSuppliers :many
SELECT sp.id, sp.company_id, sp.description, sp.categories, sp.location, sp.verified,
       sp.created_at, sp.updated_at,
       c.name AS company_name, c.verification_status
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
-- of companies, which would leak stripe_customer_id etc.). A suspended or
-- soft-deleted (GDPR-erased) company must never resolve here — this returns
-- no rows for those, which the public handler treats as not-found.
SELECT c.id AS company_id,
       c.name,
       c.verification_status,
       c.verified_domain,
       sp.description,
       sp.categories,
       sp.location
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
WHERE sp.company_id = @company_id
  AND c.status = 'active' AND c.deleted_at IS NULL;
