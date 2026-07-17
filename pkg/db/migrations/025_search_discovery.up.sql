-- Sprint 6: Postgres FTS + trigram discovery. Generated tsvector columns keep
-- the index in sync automatically (no triggers); pg_trgm gives typo tolerance
-- on company names. Additive + safe: no existing rows change semantics.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Deviation from plan: builtin array_to_string(anyarray, text) is STABLE (not
-- IMMUTABLE) on this Postgres, so a GENERATED STORED column referencing it
-- directly fails with "generation expression is not immutable". Wrap it in a
-- trivial SQL IMMUTABLE function — array_to_string's result only depends on
-- its arguments, so this is safe. (No such issue for to_tsvector(regconfig,
-- text), which is already IMMUTABLE here.)
CREATE FUNCTION tb_array_to_string_immutable(arr text[], sep text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$ SELECT array_to_string(arr, sep) $$;

ALTER TABLE supplier_profiles
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (
        to_tsvector('english',
            coalesce(description, '') || ' ' ||
            coalesce(location, '') || ' ' ||
            tb_array_to_string_immutable(categories, ' ')
        )
    ) STORED;

CREATE INDEX idx_supplier_profiles_fts ON supplier_profiles USING GIN (search_vector);

ALTER TABLE catalog_summaries
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (
        to_tsvector('english',
            coalesce(name, '') || ' ' ||
            coalesce(category, '') || ' ' ||
            coalesce(summary, '')
        )
    ) STORED;

CREATE INDEX idx_catalog_summaries_fts ON catalog_summaries USING GIN (search_vector);

CREATE INDEX idx_companies_name_trgm ON companies USING GIN (name gin_trgm_ops);
