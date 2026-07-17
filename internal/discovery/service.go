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
