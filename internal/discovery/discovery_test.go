package discovery

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func queriesOrSkip(t *testing.T) (*sqlc.Queries, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool), pool
}

func sp(s string) *string { return &s }

// A verified, well-rated, on-topic supplier must outrank an unverified,
// unrated one for the same query term.
func TestDiscoverRanksVerifiedRatedHigher(t *testing.T) {
	q, _ := queriesOrSkip(t)
	ctx := context.Background()
	tag := "rankprobe" // unique token so this test's rows are isolated

	// Verified + will receive a 5-star review.
	top, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "rankprobe-top-co", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: top.ID, Description: sp("premium " + tag + " supplier"), Categories: []string{tag}, Location: sp("NYC"),
	})
	require.NoError(t, err)
	require.NoError(t, q.SyncSupplierVerified(ctx, sqlc.SyncSupplierVerifiedParams{CompanyID: top.ID, Verified: true}))

	// Unverified, unrated, same term.
	low, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "rankprobe-low-co", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: low.ID, Description: sp("basic " + tag + " supplier"), Categories: []string{tag}, Location: sp("LA"),
	})
	require.NoError(t, err)

	svc := NewService(q)
	page, err := svc.Search(ctx, Query{Q: tag})
	require.NoError(t, err)

	var topIdx, lowIdx = -1, -1
	for i, r := range page.Results {
		if r.CompanyID == top.ID {
			topIdx = i
		}
		if r.CompanyID == low.ID {
			lowIdx = i
		}
	}
	require.GreaterOrEqual(t, topIdx, 0, "verified supplier present")
	require.GreaterOrEqual(t, lowIdx, 0, "unverified supplier present")
	require.Less(t, topIdx, lowIdx, "verified+rated ranks above unverified")
}

// The public discovery projection must expose only whitelisted keys — never
// private company columns (stripe_customer_id, type, created_at, ...).
func TestDiscoveryProjectionHasNoPrivateColumns(t *testing.T) {
	q, _ := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "projprobe-co", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: co.ID, Description: sp("projprobe supplier"), Categories: []string{"projprobe"}, Location: sp("NYC"),
	})
	require.NoError(t, err)

	page, err := NewService(q).Search(ctx, Query{Q: "projprobe"})
	require.NoError(t, err)
	require.NotEmpty(t, page.Results)

	b, err := json.Marshal(page.Results[0])
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))

	allowed := map[string]bool{
		"company_id": true, "name": true, "verification_status": true, "verified_domain": true,
		"description": true, "categories": true, "location": true, "verified": true,
		"avg_rating": true, "review_count": true, "score": true,
	}
	for k := range m {
		require.True(t, allowed[k], "unexpected public field leaked: %s", k)
	}
	forbidden := []string{"stripe_customer_id", "type", "created_at", "updated_at", "email"}
	for _, f := range forbidden {
		_, present := m[f]
		require.False(t, present, "private field must not appear: %s", f)
	}
}

// A supplier whose CATALOG (not profile) matches the query surfaces the matched
// item as a snippet.
func TestDiscoverAttachesMatchedCatalog(t *testing.T) {
	q, _ := queriesOrSkip(t)
	ctx := context.Background()
	tag := "catsnippet"

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "catsnippet-co", Type: "seller"})
	require.NoError(t, err)
	// Profile does NOT contain the tag; only the catalog item does.
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: co.ID, Description: sp("general supplier"), Categories: []string{"misc"}, Location: sp("NYC"),
	})
	require.NoError(t, err)
	_, err = q.CreateCatalogSummary(ctx, sqlc.CreateCatalogSummaryParams{
		CompanyID: co.ID, Name: tag + " widget", Category: "widgets", Summary: sp("a fine " + tag),
	})
	require.NoError(t, err)

	page, err := NewService(q).Search(ctx, Query{Q: tag})
	require.NoError(t, err)

	var found *Result
	for i := range page.Results {
		if page.Results[i].CompanyID == co.ID {
			found = &page.Results[i]
		}
	}
	require.NotNil(t, found, "catalog-only match must appear in discovery")
	require.NotEmpty(t, found.MatchedCatalog, "matched catalog snippet attached")
	require.Contains(t, found.MatchedCatalog[0].Name, tag)
}
