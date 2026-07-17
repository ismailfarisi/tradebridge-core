package activation

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func queriesOrSkip(t *testing.T) *sqlc.Queries {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool)
}

func TestFreshSellerHasStepsTodo(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "act-seller-fresh", Type: "seller"})
	require.NoError(t, err)
	st, err := NewService(q).Status(ctx, co.ID)
	require.NoError(t, err)
	require.Equal(t, "seller", st.Role)
	require.Equal(t, 0, st.PercentComplete)
	require.Len(t, st.Steps, 5)
	for _, s := range st.Steps {
		require.False(t, s.Done)
	}
}

func TestBuyerRoleSteps(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "act-buyer-fresh", Type: "buyer"})
	require.NoError(t, err)
	st, err := NewService(q).Status(ctx, co.ID)
	require.NoError(t, err)
	require.Equal(t, "buyer", st.Role)
	require.Equal(t, "first_rfq", st.Steps[0].Key)
}

func TestBothIsSellerPath(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "act-both", Type: "both"})
	require.NoError(t, err)
	st, err := NewService(q).Status(ctx, co.ID)
	require.NoError(t, err)
	require.Equal(t, "seller", st.Role)
}

func TestEmitIsIdempotent(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "act-emit", Type: "seller"})
	require.NoError(t, err)
	Emit(ctx, q, co.ID, "", EventSignup, map[string]any{"k": "v"})
	Emit(ctx, q, co.ID, "", EventSignup, nil) // repeat is a no-op, no error
	// A second distinct event is recorded.
	Emit(ctx, q, co.ID, "", EventProfileSet, nil)
	n, err := q.CountActivationEventsFor(ctx, sqlc.CountActivationEventsForParams{CompanyID: co.ID, Event: EventSignup})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

func TestEmitDedupesPerCompanyEvent(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "act-dedupe", Type: "seller"})
	require.NoError(t, err)
	Emit(ctx, q, co.ID, "", EventFirstQuote, nil)
	Emit(ctx, q, co.ID, "", EventFirstQuote, map[string]any{"again": true})
	n, err := q.CountActivationEventsFor(ctx, sqlc.CountActivationEventsForParams{CompanyID: co.ID, Event: EventFirstQuote})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

// TestSellerReaches100 seeds all five seller signals and asserts the step list
// reports 100% complete.
func TestSellerReaches100(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "act-seller-100", Type: "seller"})
	require.NoError(t, err)
	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "act-seller-100-buyer", Type: "buyer"})
	require.NoError(t, err)
	buyerUser, err := q.CreateUserWithPassword(ctx, sqlc.CreateUserWithPasswordParams{
		CompanyID: buyer.ID, Email: "act-100-buyer@example.com", Role: "owner",
	})
	require.NoError(t, err)

	// profile
	desc := "We sell widgets"
	loc := "US"
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: co.ID, Description: &desc, Categories: []string{"widgets"}, Location: &loc,
	})
	require.NoError(t, err)

	// catalog
	summary := "widget"
	_, err = q.CreateCatalogSummary(ctx, sqlc.CreateCatalogSummaryParams{
		CompanyID: co.ID, Name: "Widget", Category: "widgets", Summary: &summary,
	})
	require.NoError(t, err)

	// verified
	domain := "act-seller-100.example.com"
	require.NoError(t, q.SetCompanyVerified(ctx, sqlc.SetCompanyVerifiedParams{ID: co.ID, VerifiedDomain: &domain}))

	// agent connected
	cred, err := q.CreateAgentCredential(ctx, sqlc.CreateAgentCredentialParams{
		CompanyID: co.ID, Label: "test-agent", SecretHash: "hash",
		ExpiresAt: pgtype.Timestamptz{Valid: false},
	})
	require.NoError(t, err)
	require.NoError(t, q.TouchAgentCredentialLastSeen(ctx, cred.ID))

	// first quote (requires an RFQ)
	r, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyer.ID, CreatedBy: buyerUser.ID, Title: "Need widgets", RawText: "Need 100 widgets",
	})
	require.NoError(t, err)
	_, err = q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: r.ID, SellerCompanyID: co.ID, PriceCents: 1000, Currency: "USD",
		Notes: "", Completeness: 1.0,
	})
	require.NoError(t, err)

	st, err := NewService(q).Status(ctx, co.ID)
	require.NoError(t, err)
	require.Equal(t, "seller", st.Role)
	require.Equal(t, 100, st.PercentComplete)
	for _, s := range st.Steps {
		require.Truef(t, s.Done, "step %s should be done", s.Key)
	}
}
