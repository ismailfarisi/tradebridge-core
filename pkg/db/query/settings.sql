-- name: GetCompanySettings :one
SELECT * FROM company_settings WHERE company_id = $1;

-- name: UpsertCompanySettings :one
INSERT INTO company_settings (
    company_id, extraction_provider, extraction_model, extraction_base_url,
    extraction_api_key_enc, extraction_api_key_last4, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, NOW())
ON CONFLICT (company_id) DO UPDATE
SET extraction_provider      = EXCLUDED.extraction_provider,
    extraction_model         = EXCLUDED.extraction_model,
    extraction_base_url      = EXCLUDED.extraction_base_url,
    extraction_api_key_enc   = EXCLUDED.extraction_api_key_enc,
    extraction_api_key_last4 = EXCLUDED.extraction_api_key_last4,
    updated_at               = NOW()
RETURNING *;
