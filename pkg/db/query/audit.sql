-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE (sqlc.narg(company_id)::text IS NULL OR company_id = sqlc.narg(company_id))
  AND (sqlc.narg(actor)::text      IS NULL OR actor = sqlc.narg(actor))
  AND (sqlc.narg(action)::text     IS NULL OR action = sqlc.narg(action))
ORDER BY created_at DESC
LIMIT sqlc.arg(lim);
