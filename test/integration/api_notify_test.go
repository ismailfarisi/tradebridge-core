package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestNotify_CreateNotificationRoundTrip(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "Notify Co", Type: "buyer"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: co.ID, Email: "n1@example.com", Role: "owner"})
	require.NoError(t, err)

	n, err := q.CreateNotification(ctx, sqlc.CreateNotificationParams{
		UserID: u.ID, CompanyID: co.ID, Type: "rfq.matched",
		Title: "New RFQ matched", Body: "A buyer needs what you sell", Data: []byte(`{"rfq_id":"R1"}`),
	})
	require.NoError(t, err)
	require.Len(t, n.ID, 26)
	require.False(t, n.ReadAt.Valid)

	unread, err := q.CountUnreadNotifications(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), unread)
}

// fakeSender records sends for assertions.
type fakeSender struct {
	mu   sync.Mutex
	sent []notify.Email
}

func (f *fakeSender) Send(_ context.Context, e notify.Email) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, e)
	return nil
}
func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func TestNotify_FanOutAndPreferences(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	co, _ := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "Fan Co", Type: "seller"})
	u1, _ := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: co.ID, Email: "fan1@x.com", Role: "owner"})
	u2, _ := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: co.ID, Email: "fan2@x.com", Role: "member"})

	// u2 opts out of email for rfq.matched (in-app only).
	_, err := q.UpsertNotificationPreference(ctx, sqlc.UpsertNotificationPreferenceParams{
		UserID: u2.ID, EventType: notify.TypeRFQMatched, Email: false, InApp: true,
	})
	require.NoError(t, err)

	fs := &fakeSender{}
	n := notify.New(q, fs, "https://app.example.com", zerolog.Nop())
	n.NotifyCompany(ctx, co.ID, notify.Event{
		Type: notify.TypeRFQMatched, Title: "New RFQ matched", Body: "body",
		Data: map[string]any{"rfq_id": "R1"}, CTALabel: "View", CTAPath: "/rfqs/R1",
	})

	// Both users get an in-app row.
	require.Equal(t, int64(1), mustUnread(t, q, u1.ID))
	require.Equal(t, int64(1), mustUnread(t, q, u2.ID))

	// Email async — poll briefly. Only u1 should receive it (u2 opted out).
	require.Eventually(t, func() bool { return fs.count() == 1 }, 2*time.Second, 20*time.Millisecond)
	require.ElementsMatch(t, []string{"fan1@x.com"}, fs.sent[0].To)
}

func mustUnread(t *testing.T, q *sqlc.Queries, userID string) int64 {
	c, err := q.CountUnreadNotifications(context.Background(), userID)
	require.NoError(t, err)
	return c
}
