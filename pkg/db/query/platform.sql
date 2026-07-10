-- name: GetPlatformAdminByEmail :one
SELECT * FROM platform_admins WHERE email = $1;

-- name: CreatePlatformAdmin :one
INSERT INTO platform_admins (email, password_hash)
VALUES ($1, $2)
RETURNING *;

-- name: GetPlatformSettings :one
SELECT * FROM platform_settings WHERE id = 'global';

-- name: UpsertPlatformSettings :one
INSERT INTO platform_settings (
    id, extraction_provider, extraction_model, extraction_base_url,
    extraction_api_key_enc, extraction_api_key_last4,
    extraction_system_prompt, intake_system_prompt, updated_at
) VALUES ('global', $1, $2, $3, $4, $5, $6, $7, NOW())
ON CONFLICT (id) DO UPDATE
SET extraction_provider      = EXCLUDED.extraction_provider,
    extraction_model         = EXCLUDED.extraction_model,
    extraction_base_url      = EXCLUDED.extraction_base_url,
    extraction_api_key_enc   = EXCLUDED.extraction_api_key_enc,
    extraction_api_key_last4 = EXCLUDED.extraction_api_key_last4,
    extraction_system_prompt = EXCLUDED.extraction_system_prompt,
    intake_system_prompt     = EXCLUDED.intake_system_prompt,
    updated_at               = NOW()
RETURNING *;
