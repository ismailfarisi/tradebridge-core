-- name: DiscoverSuppliers :many
-- Weighted discovery ranking. Weights (w_rel, w_rating, w_verified) are passed
-- from Go constants (internal/discovery/rank.go) so ranking lives in ONE place
-- and is unit-testable. Relevance uses ts_rank_cd clamped to [0,1]; empty query
-- => relevance 0 (browse-all ordered by trust + recency).
WITH rep AS (
    SELECT reviewee_company_id AS company_id,
           AVG(rating)::float8  AS avg_rating,
           COUNT(*)::bigint     AS review_count
    FROM reviews
    WHERE status = 'visible'
    GROUP BY reviewee_company_id
)
SELECT c.id                       AS company_id,
       c.name                     AS name,
       c.verification_status      AS verification_status,
       c.verified_domain          AS verified_domain,
       sp.description             AS description,
       sp.categories              AS categories,
       sp.location                AS location,
       sp.verified                AS verified,
       COALESCE(rep.avg_rating, 0)::float8   AS avg_rating,
       COALESCE(rep.review_count, 0)::bigint AS review_count,
       (
            sqlc.arg(w_rel)::float8 *
              CASE WHEN sqlc.arg(query)::text = '' THEN 0
                   ELSE LEAST(ts_rank_cd(sp.search_vector,
                        websearch_to_tsquery('english', sqlc.arg(query)::text)), 1.0) END
          + sqlc.arg(w_rating)::float8 * (COALESCE(rep.avg_rating, 0) / 5.0)
          + sqlc.arg(w_verified)::float8 * (CASE WHEN sp.verified THEN 1 ELSE 0 END)
       )::float8 AS score
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
LEFT JOIN rep ON rep.company_id = sp.company_id
WHERE (
        sqlc.arg(query)::text = ''
     OR sp.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
     OR c.name ILIKE '%' || sqlc.arg(query)::text || '%'
     OR EXISTS (
            SELECT 1 FROM catalog_summaries cs
            WHERE cs.company_id = sp.company_id
              AND cs.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
        )
    )
  AND (sqlc.arg(category)::text = ''
       OR EXISTS (SELECT 1 FROM unnest(sp.categories) cat WHERE cat ILIKE sqlc.arg(category)::text))
  AND (NOT sqlc.arg(verified_only)::bool OR sp.verified)
  AND (sqlc.arg(location)::text = '' OR sp.location ILIKE '%' || sqlc.arg(location)::text || '%')
  AND (sqlc.arg(min_rating)::float8 <= 0 OR COALESCE(rep.avg_rating, 0) >= sqlc.arg(min_rating)::float8)
ORDER BY
    CASE WHEN sqlc.arg(sort)::text = 'rating' THEN COALESCE(rep.avg_rating, 0) END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort)::text = 'newest' THEN sp.updated_at END DESC NULLS LAST,
    score DESC,
    sp.updated_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: CountDiscoverSuppliers :one
-- Total matches for the same filter set (pagination). Mirrors the WHERE above;
-- no ranking/ordering.
WITH rep AS (
    SELECT reviewee_company_id AS company_id, AVG(rating)::float8 AS avg_rating
    FROM reviews WHERE status = 'visible' GROUP BY reviewee_company_id
)
SELECT COUNT(*)::bigint AS total
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
LEFT JOIN rep ON rep.company_id = sp.company_id
WHERE (
        sqlc.arg(query)::text = ''
     OR sp.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
     OR c.name ILIKE '%' || sqlc.arg(query)::text || '%'
     OR EXISTS (SELECT 1 FROM catalog_summaries cs WHERE cs.company_id = sp.company_id
                AND cs.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text))
    )
  AND (sqlc.arg(category)::text = ''
       OR EXISTS (SELECT 1 FROM unnest(sp.categories) cat WHERE cat ILIKE sqlc.arg(category)::text))
  AND (NOT sqlc.arg(verified_only)::bool OR sp.verified)
  AND (sqlc.arg(location)::text = '' OR sp.location ILIKE '%' || sqlc.arg(location)::text || '%')
  AND (sqlc.arg(min_rating)::float8 <= 0 OR COALESCE(rep.avg_rating, 0) >= sqlc.arg(min_rating)::float8);

-- name: MatchedCatalogForCompanies :many
-- Catalog items (name/category) matching the query text for a set of companies,
-- used to show WHY a supplier matched. Public columns only.
SELECT company_id, name, category
FROM catalog_summaries
WHERE company_id = ANY(sqlc.arg(company_ids)::text[])
  AND search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
ORDER BY company_id, created_at DESC;
