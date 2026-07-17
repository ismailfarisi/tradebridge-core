DROP INDEX IF EXISTS idx_companies_name_trgm;
DROP INDEX IF EXISTS idx_catalog_summaries_fts;
ALTER TABLE catalog_summaries DROP COLUMN IF EXISTS search_vector;
DROP INDEX IF EXISTS idx_supplier_profiles_fts;
ALTER TABLE supplier_profiles DROP COLUMN IF EXISTS search_vector;
DROP FUNCTION IF EXISTS tb_array_to_string_immutable(text[], text);
-- pg_trgm left installed (other features may rely on it); dropping the extension
-- is intentionally omitted to stay safe on shared databases.
