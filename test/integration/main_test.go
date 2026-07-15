package integration

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMain truncates the mutable data tables once before the suite runs, when
// pointed at a live database. Without this, companies/rfqs/etc. seeded by
// earlier full-suite runs pile up in the shared integration DB; in
// particular, once more than 25 "packaging" sellers exist,
// MatchSuppliers' LIMIT 25 (no ORDER BY) can silently omit the seller a test
// just registered, making match-dependent tests (TestA2/A3/A4,
// TestChatAttachments, TestQuoteMessage, ...) flaky depending on run history.
//
// `companies` is the root of the FK graph for all per-tenant data (users,
// invites, supplier profiles, catalog summaries, company settings, agent
// credentials/commands, audit logs, rfqs + matches, agent events, quotes +
// messages, orders, attachments), so truncating it with CASCADE clears
// everything mutable in one statement while leaving schema and the
// company-independent platform_admins/platform_settings tables alone.
func TestMain(m *testing.M) {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		pool, err := pgxpool.New(context.Background(), url)
		if err != nil {
			panic(err)
		}
		if _, err := pool.Exec(context.Background(), "TRUNCATE companies CASCADE"); err != nil {
			panic(err)
		}
		pool.Close()
	}
	os.Exit(m.Run())
}
