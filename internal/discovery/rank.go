// Package discovery implements Postgres-FTS supplier search with a transparent
// weighted rank blending text relevance, rating, and verification.
package discovery

// Rank weights — the single source of truth for discovery ranking. Passed to
// the DiscoverSuppliers query as args so scoring is tunable in one place.
const (
	WeightRelevance = 0.60
	WeightRating    = 0.25
	WeightVerified  = 0.15
)

const (
	defaultPerPage = 20
	maxPerPage     = 50
)

// Query is a normalized discovery request.
type Query struct {
	Q            string
	Category     string
	Location     string
	VerifiedOnly bool
	MinRating    float64
	Sort         string // "relevance" (default) | "rating" | "newest"
	Page         int    // 1-based
	PerPage      int
}

// normalize clamps paging and defaults sort.
func (q *Query) normalize() {
	if q.PerPage <= 0 {
		q.PerPage = defaultPerPage
	}
	if q.PerPage > maxPerPage {
		q.PerPage = maxPerPage
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	switch q.Sort {
	case "rating", "newest":
	default:
		q.Sort = "relevance"
	}
}

func (q Query) limit() int32  { return int32(q.PerPage) }
func (q Query) offset() int32 { return int32((q.Page - 1) * q.PerPage) }
