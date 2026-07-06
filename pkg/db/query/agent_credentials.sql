-- name: CreateAgentCredential :one
INSERT INTO agent_credentials (company_id, label, secret_hash)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetAgentCredentialByID :one
SELECT * FROM agent_credentials
WHERE id = $1;

-- name: ListAgentCredentialsByCompany :many
SELECT * FROM agent_credentials
WHERE company_id = $1
ORDER BY created_at DESC;

-- name: RevokeAgentCredential :exec
UPDATE agent_credentials SET status = 'revoked'
WHERE id = $1 AND company_id = $2;

-- name: TouchAgentCredentialLastSeen :exec
UPDATE agent_credentials SET last_seen = NOW()
WHERE id = $1;
