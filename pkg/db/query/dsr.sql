-- DSR (data-subject rights) export queries. Sections that already have a
-- per-company query elsewhere (users, catalog, orders, supplier profile,
-- subscription, company) are reused directly in internal/dsr/service.go — do
-- NOT duplicate them here (sqlc errors on duplicate query names). These are
-- only the sections with no existing unpaginated by-company query.

-- name: ExportRFQsByCompany :many
SELECT * FROM rfqs WHERE buyer_company_id = $1 ORDER BY created_at;

-- name: ExportQuotesByCompany :many
SELECT * FROM quotes WHERE seller_company_id = $1 ORDER BY created_at;

-- name: ExportInvoicesByCompany :many
SELECT * FROM invoices WHERE buyer_company_id = $1 OR seller_company_id = $1 ORDER BY issued_at;

-- name: ExportReviewsByCompany :many
SELECT * FROM reviews WHERE reviewer_company_id = $1 OR reviewee_company_id = $1 ORDER BY created_at;

-- name: ExportDisputesByCompany :many
SELECT * FROM disputes WHERE raised_by_company_id = $1 ORDER BY created_at;

-- name: ExportActivationEventsByCompany :many
SELECT * FROM activation_events WHERE company_id = $1 ORDER BY created_at;
