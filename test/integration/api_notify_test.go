package integration

import (
	"context"
	"net/http"
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

// panicSender simulates a Sender whose Send method panics, to prove the
// async email goroutine recovers rather than crashing the process.
type panicSender struct{}

func (panicSender) Send(context.Context, notify.Email) error {
	panic("boom")
}

func TestNotify_PanickingSenderDoesNotCrash(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "Panic Co", Type: "seller"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: co.ID, Email: "panic1@x.com", Role: "owner"})
	require.NoError(t, err)

	n := notify.New(q, panicSender{}, "https://app.example.com", zerolog.Nop())
	n.NotifyCompany(ctx, co.ID, notify.Event{
		Type: notify.TypeRFQMatched, Title: "New RFQ matched", Body: "body",
		Data: map[string]any{"rfq_id": "R1"}, CTALabel: "View", CTAPath: "/rfqs/R1",
	})

	// In-app row is written synchronously, before the (panicking) email send
	// is even attempted.
	require.Equal(t, int64(1), mustUnread(t, q, u.ID))

	// Give the background goroutine time to run Send (and panic) and recover.
	// Reaching the assertion below proves the test process survived.
	time.Sleep(200 * time.Millisecond)
	require.True(t, true, "process survived a panicking Sender")
}

func TestNotify_ListAndMarkRead(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	// Register a user to get a JWT + their ids.
	status, body := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "Inbox Co", "company_type": "buyer",
		"email": "inbox@x.com", "password": "password123",
	})
	require.Equal(t, http.StatusCreated, status)
	token := body["token"].(string)
	userID := body["user_id"].(string)
	companyID := body["company_id"].(string)

	// Seed a notification directly.
	_, err := q.CreateNotification(ctx, sqlc.CreateNotificationParams{
		UserID: userID, CompanyID: companyID, Type: "order.created",
		Title: "Order confirmed", Body: "b", Data: []byte(`{"order_id":"O1"}`),
	})
	require.NoError(t, err)

	status, list := doJSON(t, app, "GET", "/v1/notifications", token, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(1), list["unread"])
	items := list["notifications"].([]any)
	require.Len(t, items, 1)
	id := items[0].(map[string]any)["id"].(string)

	status, _ = doJSON(t, app, "POST", "/v1/notifications/"+id+"/read", token, nil)
	require.Equal(t, http.StatusNoContent, status)

	status, list = doJSON(t, app, "GET", "/v1/notifications", token, nil)
	require.Equal(t, float64(0), list["unread"])
}

// TestNotify_RFQMatchedNotifiesSeller exercises the real RFQ pipeline end to
// end: when a buyer's RFQ matches a seller's catalog, the matched seller's
// user gets an in-app rfq.matched notification (Task 7 wiring of Notifier
// into rfq.Service at the rfq.matched broadcast site).
func TestNotify_RFQMatchedNotifiesSeller(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	_, _, sellerUserID := seedSellerWithCatalog(t, app, "packaging")
	buyerToken, _ := seedVerifiedBuyer(t, app, pool, "buyer-rm-")

	st, _ := doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons needed",
		"text":  "We need 500 cartons for packaging our products, deliver to Dubai by next month.",
	})
	require.Equal(t, http.StatusCreated, st)

	require.Eventually(t, func() bool {
		c, _ := q.CountUnreadNotifications(context.Background(), sellerUserID)
		return c >= 1
	}, 3*time.Second, 25*time.Millisecond)

	// Assert the notification TYPE is rfq.matched (not just any row).
	rows, err := q.ListNotificationsByUser(context.Background(), sqlc.ListNotificationsByUserParams{UserID: sellerUserID, Limit: 1, Offset: 0})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	require.Equal(t, "rfq.matched", rows[0].Type)
}
