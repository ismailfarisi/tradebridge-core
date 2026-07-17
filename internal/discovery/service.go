package discovery

import (
	"context"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Service runs discovery queries.
type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service { return &Service{q: q} }

const maxCatalogSnippets = 3

// CatalogSnippet is a matched catalog item shown as a "why this matched" hint.
type CatalogSnippet struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

// Result is a single ranked supplier (public columns only).
type Result struct {
	CompanyID          string           `json:"company_id"`
	Name               string           `json:"name"`
	VerificationStatus string           `json:"verification_status"`
	VerifiedDomain     *string          `json:"verified_domain"`
	Description        *string          `json:"description"`
	Categories         []string         `json:"categories"`
	Location           *string          `json:"location"`
	Verified           bool             `json:"verified"`
	AvgRating          float64          `json:"avg_rating"`
	ReviewCount        int64            `json:"review_count"`
	Score              float64          `json:"score"`
	MatchedCatalog     []CatalogSnippet `json:"matched_catalog,omitempty"`
}

// Page bundles results with pagination metadata.
type Page struct {
	Results []Result `json:"results"`
	Page    int      `json:"page"`
	PerPage int      `json:"per_page"`
	Total   int64    `json:"total"`
}

// Search executes the weighted discovery query.
func (s *Service) Search(ctx context.Context, q Query) (Page, error) {
	q.normalize()
	rows, err := s.q.DiscoverSuppliers(ctx, sqlc.DiscoverSuppliersParams{
		WRel:         WeightRelevance,
		WRating:      WeightRating,
		WVerified:    WeightVerified,
		Query:        q.Q,
		Category:     q.Category,
		VerifiedOnly: q.VerifiedOnly,
		Location:     q.Location,
		MinRating:    q.MinRating,
		Sort:         q.Sort,
		Lim:          q.limit(),
		Off:          q.offset(),
	})
	if err != nil {
		return Page{}, err
	}
	total, err := s.q.CountDiscoverSuppliers(ctx, sqlc.CountDiscoverSuppliersParams{
		Query:        q.Q,
		Category:     q.Category,
		VerifiedOnly: q.VerifiedOnly,
		Location:     q.Location,
		MinRating:    q.MinRating,
	})
	if err != nil {
		return Page{}, err
	}
	results := make([]Result, 0, len(rows))
	for _, r := range rows {
		results = append(results, Result{
			CompanyID:          r.CompanyID,
			Name:               r.Name,
			VerificationStatus: r.VerificationStatus,
			VerifiedDomain:     r.VerifiedDomain,
			Description:        r.Description,
			Categories:         r.Categories,
			Location:           r.Location,
			Verified:           r.Verified,
			AvgRating:          r.AvgRating,
			ReviewCount:        r.ReviewCount,
			Score:              r.Score,
		})
	}
	if q.Q != "" && len(results) > 0 {
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.CompanyID
		}
		rows, err := s.q.MatchedCatalogForCompanies(ctx, sqlc.MatchedCatalogForCompaniesParams{
			CompanyIds: ids,
			Query:      q.Q,
		})
		if err != nil {
			return Page{}, err
		}
		byCompany := make(map[string][]CatalogSnippet, len(rows))
		for _, row := range rows {
			if len(byCompany[row.CompanyID]) >= maxCatalogSnippets {
				continue
			}
			byCompany[row.CompanyID] = append(byCompany[row.CompanyID], CatalogSnippet{Name: row.Name, Category: row.Category})
		}
		for i := range results {
			results[i].MatchedCatalog = byCompany[results[i].CompanyID]
		}
	}
	return Page{Results: results, Page: q.Page, PerPage: q.PerPage, Total: total}, nil
}

// FacetCount is a single category value with its matching count.
type FacetCount struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// VerifiedFacet buckets suppliers by verification status.
type VerifiedFacet struct {
	True  int64 `json:"true"`
	False int64 `json:"false"`
}

// RatingBucket is a "min rating and above" count.
type RatingBucket struct {
	Min   float64 `json:"min"`
	Count int64   `json:"count"`
}

// Facets bundles filter-aware facet counts for the current query.
type Facets struct {
	Categories    []FacetCount   `json:"categories"`
	Verified      VerifiedFacet  `json:"verified"`
	RatingBuckets []RatingBucket `json:"rating_buckets"`
}

// Facets returns filter-aware facet counts for the current query (each facet
// dimension omits its own filter so the user can widen it).
func (s *Service) Facets(ctx context.Context, q Query) (Facets, error) {
	q.normalize()
	cats, err := s.q.DiscoverCategoryFacets(ctx, sqlc.DiscoverCategoryFacetsParams{
		Query: q.Q, Location: q.Location, VerifiedOnly: q.VerifiedOnly, MinRating: q.MinRating,
	})
	if err != nil {
		return Facets{}, err
	}
	ver, err := s.q.DiscoverVerifiedFacets(ctx, sqlc.DiscoverVerifiedFacetsParams{
		Query: q.Q, Location: q.Location, Category: q.Category, MinRating: q.MinRating,
	})
	if err != nil {
		return Facets{}, err
	}
	rat, err := s.q.DiscoverRatingFacets(ctx, sqlc.DiscoverRatingFacetsParams{
		Query: q.Q, Location: q.Location, Category: q.Category, VerifiedOnly: q.VerifiedOnly,
	})
	if err != nil {
		return Facets{}, err
	}
	out := Facets{
		Categories: make([]FacetCount, 0, len(cats)),
		Verified:   VerifiedFacet{True: ver.VerifiedCount, False: ver.UnverifiedCount},
		RatingBuckets: []RatingBucket{
			{Min: 4, Count: rat.Gte4}, {Min: 3, Count: rat.Gte3},
			{Min: 2, Count: rat.Gte2}, {Min: 1, Count: rat.Gte1},
		},
	}
	for _, c := range cats {
		// The category value comes from unnest() over a CTE column, so sqlc
		// can't infer its Postgres type and generates it as `interface{}`;
		// pgx decodes a text column into a string underneath.
		value, _ := c.Value.(string)
		out.Categories = append(out.Categories, FacetCount{Value: value, Count: c.Count})
	}
	return out, nil
}
