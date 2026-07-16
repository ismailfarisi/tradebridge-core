-- name: UpsertDomainVerification :one
-- Claim (or re-claim) a domain: regenerates the token and resets the challenge.
INSERT INTO domain_verifications (company_id, domain, token, status, attempts)
VALUES (@company_id, @domain, @token, 'pending', 0)
ON CONFLICT (company_id, domain) DO UPDATE
SET token = EXCLUDED.token,
    status = 'pending',
    attempts = 0,
    last_checked_at = NULL,
    verified_at = NULL
RETURNING *;

-- name: GetDomainVerification :one
SELECT * FROM domain_verifications WHERE company_id = @company_id AND domain = @domain;

-- name: TouchDomainVerificationAttempt :exec
UPDATE domain_verifications
SET attempts = attempts + 1, last_checked_at = NOW()
WHERE id = @id;

-- name: MarkDomainVerificationVerified :exec
UPDATE domain_verifications SET status = 'verified', verified_at = NOW() WHERE id = @id;

-- name: SetCompanyVerified :exec
UPDATE companies
SET verification_status = 'verified', verified_domain = @verified_domain, verified_at = NOW()
WHERE id = @id;

-- name: SetCompanyVerificationStatus :exec
-- Admin override. On non-verified status, clears the verified domain so the
-- partial unique index frees it for re-verification elsewhere.
UPDATE companies
SET verification_status = @status,
    verified_domain = CASE WHEN @status::text = 'verified' THEN verified_domain ELSE NULL END,
    verified_at     = CASE WHEN @status::text = 'verified' THEN COALESCE(verified_at, NOW()) ELSE NULL END
WHERE id = @company_id;

-- name: SyncSupplierVerified :exec
UPDATE supplier_profiles SET verified = @verified, updated_at = NOW() WHERE company_id = @company_id;

-- name: FailCompanyVerifiedDomains :exec
-- Used on admin revoke/unverify: demote the company's verified challenge(s) so a
-- stale row can't let Check report verified for a domain the company no longer holds.
UPDATE domain_verifications SET status = 'failed'
WHERE company_id = @company_id AND status = 'verified';

-- name: GetCompanyVerification :one
SELECT id, verification_status, verified_domain, verified_at FROM companies WHERE id = @id;

-- name: ListPendingVerificationByCompany :many
SELECT * FROM domain_verifications
WHERE company_id = @company_id AND status = 'pending'
ORDER BY created_at DESC;

-- name: ListCompaniesByVerificationStatus :many
SELECT id, name, type, verification_status, verified_domain, verified_at
FROM companies
WHERE (sqlc.narg(status)::text IS NULL OR verification_status = sqlc.narg(status)::text)
ORDER BY verified_at DESC NULLS LAST, name
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);
