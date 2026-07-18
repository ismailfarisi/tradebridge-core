package dsr

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

func testQueriesOrSkip(t *testing.T) (*sqlc.Queries, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool), pool
}

func TestExportAssemblesCompanyFootprint(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "dsr-export-co", Type: "buyer"})
	require.NoError(t, err)

	hash := "super-secret-hash-should-never-leak"
	user, err := q.CreateUserWithPassword(ctx, sqlc.CreateUserWithPasswordParams{
		CompanyID:    co.ID,
		Email:        "dsr-export-" + co.ID + "@example.com",
		Role:         "owner",
		PasswordHash: &hash,
	})
	require.NoError(t, err)

	rfqRow, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: co.ID,
		CreatedBy:      user.ID,
		Title:          "dsr-export-rfq",
		RawText:        "need 100 widgets",
	})
	require.NoError(t, err)

	svc := NewService(q, pool)
	bundle, err := svc.Export(ctx, co.ID)
	require.NoError(t, err)

	require.NotEmpty(t, bundle["company"])
	require.NotEmpty(t, bundle["users"])
	require.NotEmpty(t, bundle["rfqs"])

	buf, err := json.Marshal(bundle)
	require.NoError(t, err)
	body := string(buf)

	require.Contains(t, body, rfqRow.ID)
	require.NotContains(t, body, hash)
	require.NotContains(t, strings.ToLower(body), "password_hash")
}

func TestDeleteSoftDeletesAnonymizesAndIsIdempotent(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "dsr-delete-co", Type: "seller"})
	require.NoError(t, err)

	hash, err := identity.HashPassword("correct-horse-battery-staple")
	require.NoError(t, err)
	origEmail := "dsr-delete-" + co.ID + "@example.com"
	user, err := q.CreateUserWithPassword(ctx, sqlc.CreateUserWithPasswordParams{
		CompanyID:    co.ID,
		Email:        origEmail,
		Role:         "owner",
		PasswordHash: &hash,
	})
	require.NoError(t, err)

	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID:   co.ID,
		Description: strPtr("we sell widgets"),
		Categories:  []string{"widgets"},
		Location:    strPtr("Dubai"),
	})
	require.NoError(t, err)

	svc := NewService(q, pool)

	require.NoError(t, svc.Delete(ctx, co.ID, user.ID))

	gate, err := q.GetCompanyGate(ctx, co.ID)
	require.NoError(t, err)
	require.True(t, gate.DeletedAt.Valid)

	company, err := q.GetCompanyByID(ctx, co.ID)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(company.Name, "Deleted company "))

	updatedUser, err := q.GetUserByID(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, "deleted+"+user.ID+"@deleted.invalid", updatedUser.Email)
	require.Nil(t, updatedUser.PasswordHash)

	profile, err := q.GetSupplierProfileByCompany(ctx, co.ID)
	require.NoError(t, err)
	require.Nil(t, profile.Description)
	require.Nil(t, profile.Location)

	// Second call is a no-op — idempotent, no error.
	require.NoError(t, svc.Delete(ctx, co.ID, user.ID))

	companyAgain, err := q.GetCompanyByID(ctx, co.ID)
	require.NoError(t, err)
	require.Equal(t, company.Name, companyAgain.Name)
}

func strPtr(s string) *string { return &s }
