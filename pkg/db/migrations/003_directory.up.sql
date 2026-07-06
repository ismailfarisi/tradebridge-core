-- Public seller directory: supplier profiles + public catalog summaries.
-- Search is served from Postgres for now (Elasticsearch deferred to Phase 2).
-- catalog_summaries.embedding (pgvector) is deferred to A2 where matching needs it.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE supplier_profiles (
    id          TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id  TEXT NOT NULL UNIQUE REFERENCES companies(id),
    description TEXT,
    categories  TEXT[] NOT NULL DEFAULT '{}',
    location    TEXT,
    verified    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE catalog_summaries (
    id          TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id  TEXT NOT NULL REFERENCES companies(id),
    name        TEXT NOT NULL,
    category    TEXT NOT NULL,
    summary     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_catalog_company ON catalog_summaries (company_id);

-- Trigram GIN indexes back ILIKE search (pg_trgm ops are IMMUTABLE, unlike a
-- to_tsvector() expression over concatenated columns). ES replaces this at scale.
CREATE INDEX idx_supplier_desc_trgm ON supplier_profiles USING GIN (description gin_trgm_ops);
CREATE INDEX idx_supplier_categories ON supplier_profiles USING GIN (categories);
CREATE INDEX idx_catalog_name_trgm ON catalog_summaries USING GIN (name gin_trgm_ops);
CREATE INDEX idx_catalog_category_trgm ON catalog_summaries USING GIN (category gin_trgm_ops);
