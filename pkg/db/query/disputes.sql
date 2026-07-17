-- name: CreateDispute :one
INSERT INTO disputes (order_id, raised_by_company_id, reason, detail)
VALUES (@order_id, @raised_by_company_id, @reason, @detail)
RETURNING *;

-- name: GetDispute :one
SELECT * FROM disputes WHERE id = @id;

-- name: ListDisputesForOrder :many
SELECT * FROM disputes WHERE order_id = @order_id ORDER BY created_at DESC;

-- name: ListDisputes :many
SELECT * FROM disputes
WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY created_at DESC;

-- name: ResolveDispute :one
-- Conditional on still-open so a double-resolve is a no-op (0 rows -> conflict).
UPDATE disputes
SET status = @status, resolution = @resolution,
    resolved_by_admin_id = @resolved_by_admin_id, resolved_at = NOW()
WHERE id = @id AND status = 'open'
RETURNING *;
