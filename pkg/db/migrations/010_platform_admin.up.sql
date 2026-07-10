-- Platform superadmin realm: a separate credential store (not tied to a
-- company) and a single global settings row controlling the LLM config +
-- system prompts used across all companies.

CREATE TABLE platform_admins (
    id            TEXT PRIMARY KEY DEFAULT gen_ulid(),
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE platform_settings (
    id                       TEXT PRIMARY KEY DEFAULT 'global' CHECK (id = 'global'),
    extraction_provider      TEXT NOT NULL DEFAULT '',
    extraction_model         TEXT NOT NULL DEFAULT '',
    extraction_base_url      TEXT NOT NULL DEFAULT '',
    extraction_api_key_enc   BYTEA,
    extraction_api_key_last4 TEXT NOT NULL DEFAULT '',
    extraction_system_prompt TEXT NOT NULL DEFAULT '',
    intake_system_prompt     TEXT NOT NULL DEFAULT '',
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
