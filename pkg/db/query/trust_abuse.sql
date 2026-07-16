-- name: CreateReport :one
INSERT INTO reports (reporter_company_id, reported_company_id, reason, detail)
VALUES (@reporter_company_id, @reported_company_id, @reason, @detail)
RETURNING *;

-- name: ListReports :many
SELECT r.*, rc.name AS reporter_name, tc.name AS reported_name
FROM reports r
JOIN companies rc ON rc.id = r.reporter_company_id
JOIN companies tc ON tc.id = r.reported_company_id
WHERE (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status)::text)
ORDER BY r.created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: ResolveReport :one
UPDATE reports SET status = @status, resolved_at = NOW() WHERE id = @id RETURNING *;

-- name: CreateBlock :one
INSERT INTO blocks (company_id, blocked_company_id)
VALUES (@company_id, @blocked_company_id)
ON CONFLICT (company_id, blocked_company_id) DO NOTHING
RETURNING *;

-- name: DeleteBlock :exec
DELETE FROM blocks WHERE company_id = @company_id AND blocked_company_id = @blocked_company_id;

-- name: ListBlocksByCompany :many
SELECT b.*, c.name AS blocked_name
FROM blocks b JOIN companies c ON c.id = b.blocked_company_id
WHERE b.company_id = @company_id
ORDER BY b.created_at DESC;

-- name: ListBlockCompanyIDsFor :many
-- Every company in a block relationship with @for_company_id, either direction.
SELECT blocked_company_id AS company_id FROM blocks WHERE blocks.company_id = @for_company_id
UNION
SELECT company_id AS company_id FROM blocks WHERE blocked_company_id = @for_company_id;
