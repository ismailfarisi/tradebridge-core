-- Per-company provider settings, editable from the frontend. Secret values are
-- stored AES-GCM-encrypted (extraction_api_key_enc); only the last 4 chars are
-- kept in clear (extraction_api_key_last4) so the UI can show a masked hint.
-- These override the process env defaults; empty falls back to env.

CREATE TABLE company_settings (
    company_id               TEXT PRIMARY KEY REFERENCES companies(id),
    -- '' | 'anthropic' | 'openai_compatible'
    extraction_provider      TEXT NOT NULL DEFAULT '',
    extraction_model         TEXT NOT NULL DEFAULT '',
    extraction_base_url      TEXT NOT NULL DEFAULT '',
    extraction_api_key_enc   BYTEA,
    extraction_api_key_last4 TEXT NOT NULL DEFAULT '',
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
