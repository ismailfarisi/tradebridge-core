# Sprint 1 — Core Comms Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `tradebridge-core` a notification + email backbone: an in-app inbox fed by the live domain events, transactional email behind a provider-agnostic interface, a soft email-verification gate, and a waitlist endpoint — all TDD, all behind the existing tenant guard.

**Architecture:** A new `internal/notify` package owns a `Notifier` (over `*sqlc.Queries` + a `Sender`) that fans company-scoped events out to per-user in-app rows (synchronous) and email (async goroutine). It is injected — matching how `eventbus.Publisher` is already wired — into `rfq`/`quotes` services, the `gateway` handler, and the `identity` handler, and called right after the existing eventbus publish at each of the three live event sites.

**Tech Stack:** Go 1.26.4, Fiber v2, pgx/v5, sqlc v1.31, golang-migrate CLI, zerolog, Resend HTTP API.

## Global Constraints

- **Go module:** `github.com/tradebridge/core`; Go `1.26.4`.
- **DB (dev):** `postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable`. Migrate via `migrate -path pkg/db/migrations -database "$DB_URL" up`. `make` is NOT installed — call `migrate`, `sqlc`, `go`, `wire` directly.
- **Integration tests** run only when `TEST_DATABASE_URL` is set (`poolOrSkip` in `test/integration/db_test.go`); `TestMain` does `TRUNCATE companies CASCADE`. Use `TEST_DATABASE_URL="postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable"`.
- **IDs:** `TEXT PRIMARY KEY DEFAULT gen_ulid()` (26-char ULID). **Timestamps:** `TIMESTAMPTZ NOT NULL DEFAULT NOW()`; nullable → `pgtype.Timestamptz` in generated Go.
- **Notifications are best-effort side effects:** `Notifier` methods never return an error and never fail the domain action — exactly like the Redis publish they ride alongside. Log failures.
- **Email default:** `LogSender` when `RESEND_API_KEY` is empty (dev/CI never break).
- **Soft gate:** unverified caller → HTTP 403 with JSON body `{"code":"verify_email","message":...}`; existing users grandfathered as verified.
- **sqlc config:** `emit_json_tags`, `emit_pointers_for_null_types`, `sql_package: pgx/v5`. Regenerate with `sqlc generate` (no DB needed to generate).
- **Spike note:** a working spike exists (`internal/notify/`, migration `014`, config edits, `pkg/db/query/{notifications,email_verifications}.sql`, regenerated sqlc). **Before executing Task 2, delete the behavioral spike** so TDD is genuine: `rm -rf internal/notify` and `git checkout internal/config/config.go` (Task 5 re-adds config test-first). **Keep** migration `014` files + the two query `.sql` files + regenerated `pkg/db/sqlc/*` (schema scaffolding is Task 1's deliverable; there is no behavior to TDD in generated code).

---

### Task 1: Schema — migrations 014 (+015 waitlist), queries, sqlc generate

**Files:**
- Create: `pkg/db/migrations/014_notifications.up.sql`, `pkg/db/migrations/014_notifications.down.sql` (already spiked — verify contents match below)
- Create: `pkg/db/migrations/015_waitlist.up.sql`, `pkg/db/migrations/015_waitlist.down.sql`
- Create: `pkg/db/query/notifications.sql`, `pkg/db/query/email_verifications.sql` (spiked), `pkg/db/query/waitlist.sql`
- Modify: add `EmailVerifiedAt` usage via regenerated `pkg/db/sqlc/*`
- Test: `test/integration/api_notify_test.go` (new; first test = a `CreateNotification` round-trip)

**Interfaces:**
- Produces (sqlc, after generate): `sqlc.CreateNotificationParams{UserID, CompanyID, Type, Title, Body string; Data []byte}` → `sqlc.Notification`; `ListNotificationsByUserParams{UserID string; Limit, Offset int32}`; `CountUnreadNotifications(ctx, userID) (int64, error)`; `MarkNotificationReadParams{ID, UserID}`; `MarkAllNotificationsRead(ctx, userID)`; `GetNotificationPreferenceParams{UserID, EventType}` → `sqlc.NotificationPreference{UserID, EventType string; Email, InApp bool}`; `UpsertNotificationPreferenceParams{UserID, EventType string; Email, InApp bool}`; `GetNotificationPreferences(ctx, userID)`; `CreateEmailVerificationParams{UserID, Token string; ExpiresAt time.Time}` → `sqlc.EmailVerification`; `GetEmailVerificationByToken(ctx, token)`; `ConsumeEmailVerification(ctx, id)`; `MarkUserEmailVerified(ctx, id)`; `CreateWaitlistSignupParams{Email string; Source *string}`; `users.EmailVerifiedAt pgtype.Timestamptz`.

- [ ] **Step 1: Write `014_notifications.up.sql`** (verify the spiked file matches exactly)

```sql
-- Sprint 1 — Notifications & email verification.
CREATE TABLE notifications (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    user_id    TEXT NOT NULL REFERENCES users(id),
    company_id TEXT NOT NULL REFERENCES companies(id),
    type       TEXT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL,
    data       JSONB,
    read_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_notifications_user ON notifications (user_id, created_at DESC);
CREATE INDEX idx_notifications_user_unread ON notifications (user_id) WHERE read_at IS NULL;

CREATE TABLE notification_preferences (
    user_id    TEXT NOT NULL REFERENCES users(id),
    event_type TEXT NOT NULL,
    email      BOOLEAN NOT NULL DEFAULT TRUE,
    in_app     BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, event_type)
);

ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;
UPDATE users SET email_verified_at = NOW() WHERE email_verified_at IS NULL;

CREATE TABLE email_verifications (
    id          TEXT PRIMARY KEY DEFAULT gen_ulid(),
    user_id     TEXT NOT NULL REFERENCES users(id),
    token       TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_email_verifications_token ON email_verifications (token) WHERE consumed_at IS NULL;
CREATE INDEX idx_email_verifications_user ON email_verifications (user_id);
```

Down (`014_notifications.down.sql`):
```sql
DROP TABLE IF EXISTS email_verifications;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
DROP TABLE IF EXISTS notification_preferences;
DROP TABLE IF EXISTS notifications;
```

- [ ] **Step 2: Write `015_waitlist.up.sql` + down**

```sql
-- Sprint 1 — public waitlist capture (switeaz.com landing → S10 launch invite).
CREATE TABLE waitlist_signups (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    email      TEXT NOT NULL UNIQUE,
    source     TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
Down: `DROP TABLE IF EXISTS waitlist_signups;`

- [ ] **Step 3: Write query files.** `pkg/db/query/notifications.sql` and `pkg/db/query/email_verifications.sql` are as spiked (see repo). Add `pkg/db/query/waitlist.sql`:

```sql
-- name: CreateWaitlistSignup :one
INSERT INTO waitlist_signups (email, source)
VALUES ($1, $2)
ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
RETURNING *;
```
(The `DO UPDATE ... RETURNING` makes re-signup idempotent — returns the existing row instead of erroring.)

Full `notifications.sql` and `email_verifications.sql` contents — verify against these:
```sql
-- notifications.sql
-- name: CreateNotification :one
INSERT INTO notifications (user_id, company_id, type, title, body, data)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;
-- name: ListNotificationsByUser :many
SELECT * FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;
-- name: CountUnreadNotifications :one
SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL;
-- name: MarkNotificationRead :exec
UPDATE notifications SET read_at = NOW() WHERE id = $1 AND user_id = $2 AND read_at IS NULL;
-- name: MarkAllNotificationsRead :exec
UPDATE notifications SET read_at = NOW() WHERE user_id = $1 AND read_at IS NULL;
-- name: GetNotificationPreferences :many
SELECT * FROM notification_preferences WHERE user_id = $1;
-- name: UpsertNotificationPreference :one
INSERT INTO notification_preferences (user_id, event_type, email, in_app, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (user_id, event_type) DO UPDATE
SET email = EXCLUDED.email, in_app = EXCLUDED.in_app, updated_at = NOW() RETURNING *;
-- name: GetNotificationPreference :one
SELECT * FROM notification_preferences WHERE user_id = $1 AND event_type = $2;
```
```sql
-- email_verifications.sql
-- name: CreateEmailVerification :one
INSERT INTO email_verifications (user_id, token, expires_at) VALUES ($1, $2, $3) RETURNING *;
-- name: GetEmailVerificationByToken :one
SELECT * FROM email_verifications WHERE token = $1;
-- name: ConsumeEmailVerification :exec
UPDATE email_verifications SET consumed_at = NOW() WHERE id = $1 AND consumed_at IS NULL;
-- name: MarkUserEmailVerified :exec
UPDATE users SET email_verified_at = NOW() WHERE id = $1 AND email_verified_at IS NULL;
```

- [ ] **Step 4: Apply migrations + generate**

Run:
```bash
cd /home/deploy/b2b/tradebridge-core
migrate -path pkg/db/migrations -database "postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable" up
sqlc generate
go build ./...
```
Expected: migrations `014`+`015` applied; `sqlc generate` silent (OK); build clean.

- [ ] **Step 5: Write the failing round-trip test** `test/integration/api_notify_test.go`

```go
package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
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
```

- [ ] **Step 6: Run it**

Run: `TEST_DATABASE_URL="postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable" go test ./test/integration/ -run TestNotify_CreateNotificationRoundTrip -v`
Expected: PASS (schema + queries exist).

- [ ] **Step 7: Commit**

```bash
git add pkg/db/migrations/014_* pkg/db/migrations/015_* pkg/db/query/notifications.sql pkg/db/query/email_verifications.sql pkg/db/query/waitlist.sql pkg/db/sqlc test/integration/api_notify_test.go
git commit -m "feat(notify): schema + queries for notifications, email verification, waitlist"
```

---

### Task 2: Email `Sender` (LogSender + ResendSender)

**Files:**
- Create: `internal/notify/sender.go`
- Test: `internal/notify/sender_test.go`

**Interfaces:**
- Produces: `Email{To []string; Subject, HTML, Text string}`; `Sender interface { Send(ctx context.Context, e Email) error }`; `LogSender{...}` (implements Sender, logs); `ResendSender{...}` (implements Sender, HTTP POST to `https://api.resend.com/emails`); `NewSender(resendAPIKey, fromAddr string, logger zerolog.Logger) Sender` → returns `LogSender` when key empty else `*ResendSender`.

- [ ] **Step 1: Write the failing test** `internal/notify/sender_test.go`

```go
package notify

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestNewSender_DefaultsToLogWhenNoKey(t *testing.T) {
	s := NewSender("", "from@x.com", zerolog.Nop())
	_, ok := s.(LogSender)
	require.True(t, ok, "empty API key should yield LogSender")
	require.NoError(t, s.Send(context.Background(), Email{To: []string{"a@b.com"}, Subject: "hi"}))
}

func TestNewSender_ResendWhenKeySet(t *testing.T) {
	s := NewSender("re_123", "from@x.com", zerolog.Nop())
	_, ok := s.(*ResendSender)
	require.True(t, ok, "non-empty API key should yield ResendSender")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/notify/ -run TestNewSender -v`
Expected: FAIL — package/`NewSender` undefined.

- [ ] **Step 3: Write `internal/notify/sender.go`**

(Use the spiked `sender.go` verbatim — it is the canonical implementation: `Email`, `Sender`, `NewSender`, `LogSender.Send` logging, `ResendSender.Send` POSTing with `Authorization: Bearer`, `resendRequest` JSON, non-2xx → error with a 512-byte body snippet, 15s HTTP client timeout.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/notify/ -run TestNewSender -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/sender.go internal/notify/sender_test.go
git commit -m "feat(notify): provider-agnostic email Sender (Resend + Log adapters)"
```

---

### Task 3: Email templates

**Files:**
- Create: `internal/notify/templates.go`
- Test: `internal/notify/templates_test.go`

**Interfaces:**
- Produces: `renderEventEmail(ev Event, baseURL string) (html, text string)` (unexported, used by Notifier); `RenderVerificationEmail(baseURL, token string) (subject, html, text string)` (exported, used by identity); `renderEmail(title, body, ctaLabel, ctaURL, baseURL string) (html, text string)`; `joinURL(base, path string) string`.
- Consumes: `Event` (defined in Task 4; for this task, add a minimal `Event` stub OR sequence Task 4 first). **Sequencing note:** implement Task 4's `Event`/const block before this task's `renderEventEmail`, OR define `Event` in `notify.go` first. Recommended order: 2 → 4 (types only) → 3. For clarity this plan keeps 3 before 4 but the `Event` type must exist; create `notify.go`'s type/const header (no methods) as Step 0 here if Task 4 is not yet done.

- [ ] **Step 1: Write the failing test** `internal/notify/templates_test.go`

```go
package notify

import (
	"strings"
	"testing"
)

func TestRenderVerificationEmail(t *testing.T) {
	subject, html, text := RenderVerificationEmail("https://app.example.com", "tok123")
	if !strings.Contains(subject, "Verify") {
		t.Errorf("subject = %q, want it to mention Verify", subject)
	}
	if !strings.Contains(html, "https://app.example.com/verify?token=tok123") {
		t.Errorf("html missing verify link: %s", html)
	}
	if !strings.Contains(text, "tok123") {
		t.Errorf("text missing token: %s", text)
	}
	// Preferences footer is mandatory on transactional mail.
	if !strings.Contains(html, "/settings/notifications") {
		t.Errorf("html missing preferences footer")
	}
}

func TestRenderEmail_EscapesHTML(t *testing.T) {
	html, _ := renderEmail("Title", "<script>alert(1)</script>", "", "", "https://x.com")
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Errorf("body HTML was not escaped: %s", html)
	}
}

func TestJoinURL(t *testing.T) {
	if got := joinURL("https://x.com/", "/a"); got != "https://x.com/a" {
		t.Errorf("joinURL = %q, want https://x.com/a", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/notify/ -run 'TestRender|TestJoinURL' -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Write `internal/notify/templates.go`**

(Use the spiked `templates.go` verbatim: `renderEventEmail`, `RenderVerificationEmail`, `renderEmail` with inline-CSS HTML shell + `html.EscapeString` on title/body/cta + preferences footer to `joinURL(baseURL, "/settings/notifications")`, and a plaintext alternative; `joinURL` trims the slash seam.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/notify/ -run 'TestRender|TestJoinURL' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/templates.go internal/notify/templates_test.go
git commit -m "feat(notify): HTML+text email templates with preferences footer"
```

---

### Task 4: `Notifier` core (fan-out + preferences)

**Files:**
- Create: `internal/notify/notify.go`
- Test: `test/integration/api_notify_test.go` (append)

**Interfaces:**
- Produces: const `TypeRFQMatched="rfq.matched"`, `TypeQuoteSubmitted="quote.submitted"`, `TypeOrderCreated="order.created"`, `TypeEmailVerify="email.verify"`; `Event{Type, Title, Body string; Data map[string]any; CTALabel, CTAPath string}`; `Notifier` with `New(q *sqlc.Queries, sender Sender, baseURL string, logger zerolog.Logger) *Notifier`; `(*Notifier).NotifyCompany(ctx, companyID string, ev Event)` (no return); `(*Notifier).SendTransactional(to, subject, html, text string)`; `(*Notifier).BaseURL() string`.
- Consumes: sqlc queries from Task 1; `Sender` from Task 2; templates from Task 3.

- [ ] **Step 1: Write the failing test** (append to `test/integration/api_notify_test.go`)

```go
// fakeSender records sends for assertions.
type fakeSender struct{ mu sync.Mutex; sent []notify.Email }
func (f *fakeSender) Send(_ context.Context, e notify.Email) error {
	f.mu.Lock(); defer f.mu.Unlock(); f.sent = append(f.sent, e); return nil
}
func (f *fakeSender) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.sent) }

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
```
Add imports: `sync`, `time`, `github.com/rs/zerolog`, `github.com/tradebridge/core/internal/notify`.

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL="postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable" go test ./test/integration/ -run TestNotify_FanOut -v`
Expected: FAIL — `notify.New` undefined.

- [ ] **Step 3: Write `internal/notify/notify.go`**

(Use the spiked `notify.go` verbatim: package doc; the four `Type*` consts; `Event`; `Notifier{q, sender, baseURL, logger}`; `New`; `NotifyCompany` — `ListUsersByCompany`, `encodeData`, per-user `channels(ctx,userID,eventType)` defaulting to both-on when `pgx.ErrNoRows`, synchronous `CreateNotification` when in-app, collect emails, then `dispatchEmail` once; `SendTransactional`; `BaseURL`; `channels`; `encodeData` folding `cta_label`/`cta_path` into data; `dispatchEmail` goroutine w/ `context.WithTimeout(context.Background(), 15s)`.)

- [ ] **Step 4: Run to verify it passes**

Run: `TEST_DATABASE_URL="postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable" go test ./test/integration/ -run TestNotify_FanOut -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/notify.go test/integration/api_notify_test.go
git commit -m "feat(notify): Notifier fan-out with per-user preference honoring"
```

---

### Task 5: Notifications HTTP handler + config + router wiring

**Files:**
- Create: `internal/notify/handler.go`
- Modify: `internal/config/config.go` (add `ResendAPIKey`, `EmailFrom`, `AppBaseURL`, `SentryDSN` fields + defaults)
- Modify: `internal/httpapi/router.go` (add config fields to `Config`; build `sender`+`notifier`; register notify handler; keep a `*notify.Notifier` in scope for later tasks)
- Modify: `cmd/api/main.go` (pass new config through)
- Test: `test/integration/api_notify_test.go` (append HTTP-level tests)

**Interfaces:**
- Produces: `notify.NewHandler(q *sqlc.Queries) *notify.Handler`; `(*Handler).Register(app *fiber.App, guard ...fiber.Handler)` mounting `GET /v1/notifications`, `POST /v1/notifications/:id/read`, `POST /v1/notifications/read-all`, `GET /v1/notifications/preferences`, `PUT /v1/notifications/preferences`. List response: `{"notifications":[{id,type,title,body,data,read,created_at}],"unread":N}`.
- Produces (router): a constructed `*notify.Notifier` (`notifier`) available to Tasks 6–9; `httpapi.Config` gains `ResendAPIKey, EmailFrom, AppBaseURL string`.

- [ ] **Step 1: Write the failing HTTP test** (append)

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./test/integration/ -run TestNotify_ListAndMarkRead -v`
Expected: FAIL — 404 (route not mounted).

- [ ] **Step 3: Write `internal/notify/handler.go`**

(Use the spiked `handler.go` verbatim: `Handler{q}`, `NewHandler`, `Register` mounting the five routes, `list` (limit/offset clamp, `unread` count, `notificationView` with `Read: r.ReadAt.Valid` and RFC3339 `created_at`), `markRead`/`readAll` (scoped to `middleware.UserID`), `getPreferences`/`putPreference` (validate event type via `validEventType`), `clampInt`.)

- [ ] **Step 4: Add config fields** — `internal/config/config.go` (already spiked for the first three; add `SentryDSN`):

```go
// in Config struct:
ResendAPIKey string `mapstructure:"RESEND_API_KEY"`
EmailFrom    string `mapstructure:"EMAIL_FROM"`
AppBaseURL   string `mapstructure:"APP_BASE_URL"`
SentryDSN    string `mapstructure:"SENTRY_DSN"`
```
```go
// in Load() defaults:
v.SetDefault("RESEND_API_KEY", "")
v.SetDefault("EMAIL_FROM", "TradeBridge <no-reply@switeaz.com>")
v.SetDefault("APP_BASE_URL", "http://localhost:3010")
v.SetDefault("SENTRY_DSN", "")
```

- [ ] **Step 5: Wire into `internal/httpapi/router.go`.** Add to `Config`:
```go
ResendAPIKey string
EmailFrom    string
AppBaseURL   string
```
After `pub := eventbus.New(q, cfg.Redis, cfg.Logger)` add:
```go
sender := notify.NewSender(cfg.ResendAPIKey, cfg.EmailFrom, cfg.Logger)
notifier := notify.New(q, sender, cfg.AppBaseURL, cfg.Logger)
notify.NewHandler(q).Register(f, guard...)
```
Add `"github.com/tradebridge/core/internal/notify"` to imports. (`notifier` is unused until Task 6 — to keep the build green in this task, pass it to `identity.NewHandler` in Task 6; for now add `_ = notifier` with a `// wired into services in Tasks 6–9` comment, removed in Task 6.)

- [ ] **Step 6: Thread config in `cmd/api/main.go`** — add to the `httpapi.Config{...}` literal:
```go
ResendAPIKey: app.Config.ResendAPIKey,
EmailFrom:    app.Config.EmailFrom,
AppBaseURL:   app.Config.AppBaseURL,
```

- [ ] **Step 7: Run to verify it passes**

Run: `go build ./... && TEST_DATABASE_URL=... go test ./test/integration/ -run TestNotify_ListAndMarkRead -v`
Expected: build clean; PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/notify/handler.go internal/config/config.go internal/httpapi/router.go cmd/api/main.go test/integration/api_notify_test.go
git commit -m "feat(notify): /v1/notifications API + config + router wiring"
```

---

### Task 6: Email verification + soft gate

**Files:**
- Create: `pkg/middleware/verified.go`
- Test: `pkg/middleware/verified_test.go` (unit, no DB) + `test/integration/api_verify_test.go`
- Modify: `internal/identity/handler.go` (register sends verify email; add verify-email + resend routes; needs a `*notify.Notifier`), `internal/httpapi/router.go` (pass `notifier` to `identity.NewHandler`; mount `RequireVerified` on RFQ create).

**Interfaces:**
- Produces: `identity.NewHandler(q *sqlc.Queries, tokens *TokenService, notifier *notify.Notifier) *Handler` (signature change — the sole caller is router.go); routes `POST /v1/auth/verify-email {token}` (public) and `POST /v1/auth/resend-verification` (guarded); `middleware.RequireVerified(q *sqlc.Queries) fiber.Handler` → 403 `{code:"verify_email"}` when caller's `email_verified_at` is NULL. Runs after `TenantGuard`.
- Consumes: `notify.RenderVerificationEmail`, `notify.SendTransactional`, `randomToken()` (existing in identity), sqlc `CreateEmailVerification`/`GetEmailVerificationByToken`/`ConsumeEmailVerification`/`MarkUserEmailVerified`, `GetUserByID`.

- [ ] **Step 1: Write the failing middleware unit test** `pkg/middleware/verified_test.go`

```go
package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

// isVerifiedGate is the pure decision extracted for unit testing: given whether
// the user's email is verified, should the request pass?
func TestVerifiedGateDecision(t *testing.T) {
	require.True(t, allowVerified(true))
	require.False(t, allowVerified(false))
}

// Smoke: a handler behind a stub gate returning 403 shape.
func TestVerifyErrorShape(t *testing.T) {
	app := fiber.New()
	app.Get("/x", func(c *fiber.Ctx) error { return verifyEmailError() })
	req := httptest.NewRequest("GET", "/x", nil)
	resp, _ := app.Test(req)
	require.Equal(t, 403, resp.StatusCode)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/middleware/ -run 'TestVerified|TestVerifyError' -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Write `pkg/middleware/verified.go`**

```go
package middleware

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// allowVerified is the pure gate decision (extracted for testing).
func allowVerified(emailVerified bool) bool { return emailVerified }

// verifyEmailError is the 403 returned to unverified callers. The `code` lets
// the frontend show the "verify your email" banner instead of a raw error.
func verifyEmailError() error {
	return fiber.NewError(fiber.StatusForbidden, "verify_email")
}

// RequireVerified must run after TenantGuard. It blocks the request when the
// acting user has not verified their email (soft gate). Existing users are
// grandfathered (email_verified_at backfilled), so this only stops brand-new
// unverified signups from the gated action.
func RequireVerified(q *sqlc.Queries) fiber.Handler {
	return func(c *fiber.Ctx) error {
		u, err := q.GetUserByID(c.Context(), UserID(c))
		if errors.Is(err, pgx.ErrNoRows) {
			return fiber.NewError(fiber.StatusUnauthorized, "unknown user")
		} else if err != nil {
			return err
		}
		if !allowVerified(u.EmailVerifiedAt.Valid) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"code": "verify_email", "message": "verify your email to continue",
			})
		}
		return c.Next()
	}
}
```

- [ ] **Step 4: Run middleware unit test**

Run: `go test ./pkg/middleware/ -run 'TestVerified|TestVerifyError' -v`
Expected: PASS. (Note `verifyEmailError` returns a Fiber error → default 403 body; the `TestVerifyErrorShape` asserts only the status.)

- [ ] **Step 5: Write the failing integration test** `test/integration/api_verify_test.go`

```go
package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerify_SoftGateOnRFQCreate(t *testing.T) {
	app := buildAPI(t)

	status, body := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "Gate Co", "company_type": "buyer",
		"email": "gate@x.com", "password": "password123",
	})
	require.Equal(t, http.StatusCreated, status)
	token := body["token"].(string)

	// New user is unverified → RFQ create is blocked with the verify_email code.
	status, gated := doJSON(t, app, "POST", "/v1/rfqs", token, map[string]any{
		"title": "Need 500 units", "raw_text": "500 units of widget",
	})
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "verify_email", gated["code"])

	// Fetch the verification token from the DB (the email is logged, not returned).
	pool := poolOrSkip(t)
	var vtoken string
	err := pool.QueryRow(t.Context(),
		`SELECT ev.token FROM email_verifications ev
		 JOIN users u ON u.id = ev.user_id WHERE u.email = $1`, "gate@x.com").Scan(&vtoken)
	require.NoError(t, err)

	// Verify, then RFQ create succeeds.
	status, _ = doJSON(t, app, "POST", "/v1/auth/verify-email", "", map[string]any{"token": vtoken})
	require.Equal(t, http.StatusOK, status)

	status, _ = doJSON(t, app, "POST", "/v1/rfqs", token, map[string]any{
		"title": "Need 500 units", "raw_text": "500 units of widget",
	})
	require.Equal(t, http.StatusCreated, status)
}
```
(If `t.Context()` is unavailable on the Go version, use `context.Background()`.)

- [ ] **Step 6: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./test/integration/ -run TestVerify_SoftGate -v`
Expected: FAIL — RFQ create returns 201 (no gate yet) / no verify route.

- [ ] **Step 7: Implement.** In `internal/identity/handler.go`:
  - Change `NewHandler(q, tokens)` → `NewHandler(q, tokens, notifier *notify.Notifier)`; store `notifier`.
  - In `Register`, add `auth.Post("/verify-email", h.verifyEmail)` and `users.Post("/resend-verification", h.resendVerification)` — wait, resend needs the user's own id: mount under the guarded group. Add to `auth` (public): `verify-email`; add to a guarded group: `resend-verification`.
  - In `register`, after the user is created and before `issue`, call `h.sendVerification(ctx, user)`.
  - Add:
```go
func (h *Handler) sendVerification(ctx context.Context, u sqlc.User) {
	token, err := randomToken()
	if err != nil { return }
	if _, err := h.q.CreateEmailVerification(ctx, sqlc.CreateEmailVerificationParams{
		UserID: u.ID, Token: token, ExpiresAt: time.Now().Add(48 * time.Hour),
	}); err != nil { return }
	subject, html, text := notify.RenderVerificationEmail(h.notifier.BaseURL(), token)
	h.notifier.SendTransactional(u.Email, subject, html, text)
}

type verifyEmailReq struct { Token string `json:"token"` }

func (h *Handler) verifyEmail(c *fiber.Ctx) error {
	var req verifyEmailReq
	if err := c.BodyParser(&req); err != nil || req.Token == "" {
		return fiber.NewError(fiber.StatusBadRequest, "token required")
	}
	ctx := c.Context()
	ev, err := h.q.GetEmailVerificationByToken(ctx, req.Token)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "invalid or expired token")
	} else if err != nil {
		return err
	}
	if ev.ConsumedAt.Valid || time.Now().After(ev.ExpiresAt) {
		return fiber.NewError(fiber.StatusBadRequest, "token already used or expired")
	}
	if err := h.q.ConsumeEmailVerification(ctx, ev.ID); err != nil { return err }
	if err := h.q.MarkUserEmailVerified(ctx, ev.UserID); err != nil { return err }
	return c.JSON(fiber.Map{"verified": true})
}

func (h *Handler) resendVerification(c *fiber.Ctx) error {
	u, err := h.q.GetUserByID(c.Context(), middleware.UserID(c))
	if err != nil { return err }
	if u.EmailVerifiedAt.Valid {
		return c.JSON(fiber.Map{"verified": true})
	}
	h.sendVerification(c.Context(), u)
	return c.JSON(fiber.Map{"sent": true})
}
```
  Add imports `context`, `notify`, ensure `time`, `errors`, `pgx`, `middleware` present.
  - In `router.go`: `identity.NewHandler(q, tokens, notifier).Register(f, guard...)` and remove the `_ = notifier` placeholder. Gate RFQ create: the cleanest mount is inside `rfq`'s Register via an extra guard, but to avoid changing rfq's signature, wrap at the router by mounting a `RequireVerified` on the create route. Since `rfq.NewHandler(...).Register` owns `/v1/rfqs`, add the gate **inside** `rfq/handler.go` Register by accepting an optional extra middleware. Simplest per-plan approach: add a parameter-free guard in Register — change `g.Post("/", h.create)` to `g.Post("/", append(extra, h.create)...)` where `extra` is passed to `Register`. **Chosen approach:** add `verified fiber.Handler` param to `rfq.NewHandler` is heavier; instead mount the gate at the router by re-registering: after `rfq...Register`, the route already exists. To keep it simple and explicit, change `rfq.Handler.Register` signature to `Register(app, verified fiber.Handler, guard ...fiber.Handler)` and apply `verified` only to `create`, `chat`, `chatStream`. Update the sole caller in router.go to pass `middleware.RequireVerified(q)`.

- [ ] **Step 8: Run to verify it passes**

Run: `go build ./... && TEST_DATABASE_URL=... go test ./test/integration/ -run 'TestVerify_SoftGate' ./pkg/middleware/ -run 'TestVerified|TestVerifyError' -v`
Expected: build clean; PASS. Then run the full identity/rfq suites to catch signature breaks: `TEST_DATABASE_URL=... go test ./... 2>&1 | tail -20` — all green.

- [ ] **Step 9: Commit**

```bash
git add internal/identity/handler.go internal/httpapi/router.go internal/rfq/handler.go pkg/middleware/verified.go pkg/middleware/verified_test.go test/integration/api_verify_test.go
git commit -m "feat(identity): email verification + soft gate on RFQ create"
```

---

### Task 7: Event wiring — `rfq.matched` → seller

**Files:**
- Modify: `internal/rfq/service.go` (add `notifier *notify.Notifier` to `NewService`; call `NotifyCompany` per matched seller after publish), `internal/httpapi/router.go` (pass notifier), `internal/rfq/handler_stream_test.go` (add `nil` notifier arg to the two `NewService` calls).
- Test: `test/integration/api_notify_test.go` (append event assertion)

**Interfaces:**
- Produces: `rfq.NewService(q, pub, resolver, loader, logger, ttlDays, llmHealth, notifier *notify.Notifier)` (trailing param).
- Consumes: `notify.TypeRFQMatched`.

- [ ] **Step 1: Write the failing test** (append to `api_notify_test.go`) — assert that after an RFQ is created and matched (reuse the A2 pattern), the matched seller's user has an `rfq.matched` notification. (Model it on `TestA2_RFQCreateMatchAndBroadcast` in `api_a2_test.go`: register a seller w/ catalog, register a buyer, verify the buyer, POST `/v1/rfqs`, then assert `CountUnreadNotifications(sellerUserID) >= 1` and the newest row's `Type == "rfq.matched"`.)

```go
func TestNotify_RFQMatchedNotifiesSeller(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)

	sellerUserID := seedSellerWithCatalog(t, app, q, "packaging boxes") // helper mirrors A2 setup
	buyerToken := seedVerifiedBuyer(t, app, q, "buyer-rm@x.com")

	status, _ := doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Need packaging boxes", "raw_text": "500 packaging boxes",
	})
	require.Equal(t, http.StatusCreated, status)

	require.Eventually(t, func() bool {
		c, _ := q.CountUnreadNotifications(context.Background(), sellerUserID)
		return c >= 1
	}, 2*time.Second, 25*time.Millisecond)
}
```
(Define `seedSellerWithCatalog` / `seedVerifiedBuyer` helpers in the test file by lifting the exact registration+catalog+verify calls used in `api_a2_test.go`; `seedVerifiedBuyer` registers then flips `email_verified_at` via `q` or the verify endpoint.)

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./test/integration/ -run TestNotify_RFQMatchedNotifiesSeller -v`
Expected: FAIL — no notification created (or compile error once signature changes).

- [ ] **Step 3: Implement.** In `internal/rfq/service.go`:
  - Add `notifier *notify.Notifier` field + trailing `NewService` param; import `notify`.
  - In the `broadcast`/match loop, after `s.pub.Publish(ctx, m.SellerCompanyID, "rfq.matched", body)` succeeds, add:
```go
if s.notifier != nil {
	s.notifier.NotifyCompany(ctx, m.SellerCompanyID, notify.Event{
		Type:  notify.TypeRFQMatched,
		Title: "New RFQ matched: " + rfq.Title,
		Body:  "A buyer's request matches your catalog. Open it to quote.",
		Data:  map[string]any{"rfq_id": rfq.ID},
		CTALabel: "View RFQ", CTAPath: "/rfqs/" + rfq.ID,
	})
}
```
  - `router.go`: append `notifier` to the `rfq.NewService(...)` call.
  - `internal/rfq/handler_stream_test.go`: add `, nil` as the final arg to both `NewService(...)` calls.

- [ ] **Step 4: Run to verify it passes**

Run: `go build ./... && TEST_DATABASE_URL=... go test ./test/integration/ -run TestNotify_RFQMatched -v && go test ./internal/rfq/ -v`
Expected: build clean; PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rfq/service.go internal/rfq/handler_stream_test.go internal/httpapi/router.go test/integration/api_notify_test.go
git commit -m "feat(notify): rfq.matched notifies the seller"
```

---

### Task 8: Event wiring — `order.created` → buyer + seller

**Files:**
- Modify: `internal/quotes/service.go` (add `notifier` to `NewService`; notify both companies after commit), `internal/httpapi/router.go` (pass notifier)
- Test: `test/integration/api_notify_test.go` (append)

**Interfaces:**
- Produces: `quotes.NewService(q, pool, pub, loader, logger, notifier *notify.Notifier)` (trailing param).
- Consumes: `notify.TypeOrderCreated`.

- [ ] **Step 1: Write the failing test** (append) — after a buyer accepts a quote (reuse `api_a4_test.go` acceptance flow), assert both the buyer's user and the seller's user get an `order.created` notification.

```go
func TestNotify_OrderCreatedNotifiesBothSides(t *testing.T) {
	// ... set up matched RFQ + submitted quote (mirror TestA4), capture buyerUserID + sellerUserID ...
	// accept the quote:
	status, _ := doJSON(t, app, "POST", "/v1/quotes/"+quoteID+"/accept", buyerToken, nil)
	require.Equal(t, http.StatusOK, status)

	require.Eventually(t, func() bool {
		bc, _ := q.CountUnreadNotifications(context.Background(), buyerUserID)
		sc, _ := q.CountUnreadNotifications(context.Background(), sellerUserID)
		return bc >= 1 && sc >= 1
	}, 2*time.Second, 25*time.Millisecond)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./test/integration/ -run TestNotify_OrderCreated -v`
Expected: FAIL.

- [ ] **Step 3: Implement.** In `internal/quotes/service.go` `Accept`, after `tx.Commit` and the existing `PublishExisting` calls, add:
```go
if s.notifier != nil {
	ev := notify.Event{
		Type: notify.TypeOrderCreated, Title: "Order confirmed",
		Body: "A purchase order was created from the accepted quote.",
		Data: map[string]any{"order_id": order.ID, "rfq_id": order.RfqID},
		CTALabel: "View order", CTAPath: "/orders/" + order.ID,
	}
	s.notifier.NotifyCompany(ctx, order.BuyerCompanyID, ev)
	s.notifier.NotifyCompany(ctx, order.SellerCompanyID, ev)
}
```
Add `notifier` field + `NewService` trailing param + `notify` import; append `notifier` at the `quotes.NewService(...)` call in `router.go`.

- [ ] **Step 4: Run to verify it passes**

Run: `go build ./... && TEST_DATABASE_URL=... go test ./test/integration/ -run TestNotify_OrderCreated -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/quotes/service.go internal/httpapi/router.go test/integration/api_notify_test.go
git commit -m "feat(notify): order.created notifies buyer and seller"
```

---

### Task 9: Event wiring — `quote.submitted` → buyer (gateway)

**Files:**
- Modify: `internal/gateway/gateway.go` (add `notifier *notify.Notifier` field + `NewHandler` trailing param), `internal/gateway/commands.go` (after `quotes.Record` for `quote.submit`, notify the buyer), `cmd/gateway/main.go` (build sender+notifier, pass it), `test/integration/api_a3_test.go` (add `nil` notifier arg to the two `gateway.NewHandler` calls)
- Test: `test/integration/api_gateway_notify_test.go` (new)

**Interfaces:**
- Produces: `gateway.NewHandler(q, rdb, store, logger, rlc, notifier *notify.Notifier)` (trailing param).
- Consumes: `notify.TypeQuoteSubmitted`; `GetRFQ` (for `BuyerCompanyID`); a count of submitted quotes for the "N suppliers quoted" copy.

- [ ] **Step 1: Write the failing test** `api_gateway_notify_test.go` — mirror `api_a3_test.go` (agent submits a quote via `POST /agent/v1/commands`), then assert the buyer company's user has a `quote.submitted` notification.

```go
func TestNotify_QuoteSubmittedNotifiesBuyer(t *testing.T) {
	// build gateway handler WITH a notifier (fake or real over pool)
	// ... register buyer + seller, buyer posts RFQ (matched to seller), capture buyerUserID + rfqID + agent creds ...
	// agent submits quote:
	status := postAgentCommand(t, gw, agentCred, map[string]any{
		"type": "quote.submit", "rfq_id": rfqID, "idempotency_key": "k1",
		"payload": map[string]any{"price": 1200, "currency": "USD"},
	})
	require.Equal(t, http.StatusCreated, status)

	require.Eventually(t, func() bool {
		c, _ := q.CountUnreadNotifications(context.Background(), buyerUserID)
		return c >= 1
	}, 2*time.Second, 25*time.Millisecond)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./test/integration/ -run TestNotify_QuoteSubmitted -v`
Expected: FAIL.

- [ ] **Step 3: Implement.** In `internal/gateway/gateway.go`: add `notifier *notify.Notifier` to `Handler` + `NewHandler` (trailing param; may be nil). In `commands.go`, in the `case "quote.submit":` block, after `quotes.Record(...)` succeeds, add:
```go
if h.notifier != nil {
	if r, err := h.q.GetRFQ(c.Context(), *rfqID); err == nil {
		n := h.countSubmittedQuotes(c.Context(), *rfqID) // small helper via existing query or COUNT
		h.notifier.NotifyCompany(c.Context(), r.BuyerCompanyID, notify.Event{
			Type: notify.TypeQuoteSubmitted,
			Title: quoteCountTitle(n), // "A supplier quoted your RFQ" / "N suppliers quoted your RFQ"
			Body: "Open your RFQ to compare quotes.",
			Data: map[string]any{"rfq_id": r.ID},
			CTALabel: "Compare quotes", CTAPath: "/rfqs/" + r.ID,
		})
	}
}
```
Add a `quoteCountTitle(n int64) string` helper (singular/plural) and `countSubmittedQuotes` (use an existing count query if present, else add `CountSubmittedQuotesByRFQ` to `pkg/db/query/quotes.sql` + regenerate). In `cmd/gateway/main.go`, build `sender := notify.NewSender(...)`, `notifier := notify.New(app.Queries, sender, app.Config.AppBaseURL, app.Logger)`, pass to `NewHandler`. Add `, nil` to the two `gateway.NewHandler` calls in `api_a3_test.go`.

- [ ] **Step 4: Run to verify it passes**

Run: `go build ./... && TEST_DATABASE_URL=... go test ./test/integration/ -run 'TestNotify_QuoteSubmitted|TestA3' -v`
Expected: build clean; PASS (A3 still green with the new nil arg).

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/gateway.go internal/gateway/commands.go cmd/gateway/main.go pkg/db/query/quotes.sql pkg/db/sqlc test/integration/api_a3_test.go test/integration/api_gateway_notify_test.go
git commit -m "feat(notify): quote.submitted notifies the buyer (gateway)"
```

---

### Task 10: Waitlist endpoint

**Files:**
- Create: `internal/waitlist/handler.go`
- Modify: `internal/httpapi/router.go` (register public route)
- Test: `test/integration/api_waitlist_test.go`

**Interfaces:**
- Produces: `waitlist.NewHandler(q *sqlc.Queries) *waitlist.Handler`; `(*Handler).Register(app *fiber.App)` mounting **public** `POST /v1/waitlist {email, source?}` → 200 `{"joined":true}` (idempotent on duplicate).
- Consumes: sqlc `CreateWaitlistSignup`.

- [ ] **Step 1: Write the failing test** `api_waitlist_test.go`

```go
func TestWaitlist_JoinIsIdempotent(t *testing.T) {
	app := buildAPI(t)
	for i := 0; i < 2; i++ {
		status, body := doJSON(t, app, "POST", "/v1/waitlist", "", map[string]any{
			"email": "early@x.com", "source": "landing",
		})
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, true, body["joined"])
	}
	status, _ := doJSON(t, app, "POST", "/v1/waitlist", "", map[string]any{"email": "not-an-email"})
	require.Equal(t, http.StatusBadRequest, status)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./test/integration/ -run TestWaitlist_Join -v`
Expected: FAIL — 404.

- [ ] **Step 3: Write `internal/waitlist/handler.go`**

```go
package waitlist

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

type Handler struct{ q *sqlc.Queries }

func NewHandler(q *sqlc.Queries) *Handler { return &Handler{q: q} }

// Register mounts the PUBLIC waitlist route (no tenant guard — pre-signup).
func (h *Handler) Register(app *fiber.App) {
	app.Post("/v1/waitlist", h.join)
}

type joinReq struct {
	Email  string `json:"email"`
	Source string `json:"source"`
}

func (h *Handler) join(c *fiber.Ctx) error {
	var req joinReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") || len(email) < 5 {
		return fiber.NewError(fiber.StatusBadRequest, "valid email required")
	}
	var source *string
	if s := strings.TrimSpace(req.Source); s != "" {
		source = &s
	}
	if _, err := h.q.CreateWaitlistSignup(c.Context(), sqlc.CreateWaitlistSignupParams{
		Email: email, Source: source,
	}); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"joined": true})
}
```
Register in `router.go`: `waitlist.NewHandler(q).Register(f)` (public, near the `/health` route — no guard). Add import.

- [ ] **Step 4: Run to verify it passes**

Run: `go build ./... && TEST_DATABASE_URL=... go test ./test/integration/ -run TestWaitlist_Join -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/waitlist/handler.go internal/httpapi/router.go test/integration/api_waitlist_test.go
git commit -m "feat(waitlist): public POST /v1/waitlist (idempotent capture)"
```

---

### Task 11: E2E #1 — signup → verify → RFQ → notification

**Files:**
- Create: `test/integration/e2e_01_signup_to_notification_test.go`

**Interfaces:**
- Consumes: everything above. This is the first test in the per-sprint E2E suite; S10's launch gate is "this suite still green."

- [ ] **Step 1: Write the E2E test**

```go
package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// E2E #1: a new buyer signs up, is soft-gated, verifies email, posts an RFQ that
// matches a seller, and the seller receives an in-app notification. This is the
// spine of the Sprint 1 journey and the first member of the growing E2E suite.
func TestE2E01_SignupVerifyRFQNotification(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	sellerUserID := seedSellerWithCatalog(t, app, q, "packaging boxes")

	// Buyer signs up (unverified).
	_, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "E2E Buyer", "company_type": "buyer",
		"email": "e2e-buyer@x.com", "password": "password123",
	})
	buyerToken := reg["token"].(string)

	// Soft gate blocks RFQ create.
	status, gated := doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Need packaging boxes", "raw_text": "500 packaging boxes",
	})
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "verify_email", gated["code"])

	// Verify.
	var vtoken string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT token FROM email_verifications ev JOIN users u ON u.id=ev.user_id WHERE u.email=$1`,
		"e2e-buyer@x.com").Scan(&vtoken))
	status, _ = doJSON(t, app, "POST", "/v1/auth/verify-email", "", map[string]any{"token": vtoken})
	require.Equal(t, http.StatusOK, status)

	// RFQ create now succeeds and matches the seller.
	status, _ = doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Need packaging boxes", "raw_text": "500 packaging boxes",
	})
	require.Equal(t, http.StatusCreated, status)

	// Seller is notified.
	require.Eventually(t, func() bool {
		c, _ := q.CountUnreadNotifications(ctx, sellerUserID)
		return c >= 1
	}, 3*time.Second, 25*time.Millisecond)
}
```
(Reuse the `seedSellerWithCatalog` helper from Task 7. Add `time` import.)

- [ ] **Step 2: Run**

Run: `TEST_DATABASE_URL=... go test ./test/integration/ -run TestE2E01 -v`
Expected: PASS.

- [ ] **Step 3: Full-suite green check**

Run: `go build ./... && TEST_DATABASE_URL="postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable" go test ./... 2>&1 | tail -30`
Expected: all packages PASS (no signature regressions).

- [ ] **Step 4: Commit**

```bash
git add test/integration/e2e_01_signup_to_notification_test.go
git commit -m "test(e2e): #1 signup -> verify -> RFQ -> seller notification"
```

---

## Self-Review

**Spec coverage:**
- WS1 notifications backbone → Tasks 1, 4, 5. ✓
- WS2 transactional email → Tasks 2, 3 (code); DKIM/SPF/DMARC ops are a runbook, not code — tracked in the spec's provisioning list, not a plan task (no code deliverable). ✓ (flagged, not silently dropped)
- WS3 verification soft gate → Task 6. ✓
- WS4 event wiring → Tasks 7, 8, 9. ✓
- WS5 waitlist → Tasks 1 (schema) + 10 (endpoint). Landing page is frontend → Plan 2. ✓
- WS6 shared UI / notification center → **Plan 2** (frontend). ✓ (out of this plan by scope split)
- WS7 CI + Sentry → **Plan 3** (backbone); config `SENTRY_DSN` field added in Task 5 so it's ready. ✓
- WS8 E2E #1 → Task 11. ✓

**Placeholder scan:** No TBD/TODO. Task 3 flags a real sequencing dependency (`Event` must exist before `renderEventEmail`) with the resolution stated. Task 6 Step 7 and Task 9 Step 3 describe helper additions (`quoteCountTitle`, `countSubmittedQuotes`, `seed*` test helpers) — these are named with signatures and behavior; the executor writes the small body. Acceptable (named + typed + behavior specified), not a blind "add logic here."

**Type consistency:** `Notifier.NotifyCompany(ctx, companyID string, ev Event)` used identically in Tasks 4/7/8/9. `Event` fields (`Type/Title/Body/Data/CTALabel/CTAPath`) consistent throughout. Constructor trailing-`notifier`-param convention consistent across `rfq.NewService`, `quotes.NewService`, `gateway.NewHandler`, `identity.NewHandler`. `RequireVerified(q)` returns `fiber.Handler`, mounted after `TenantGuard`. Response shapes (`{notifications,unread}`, `{joined:true}`, `{code:"verify_email"}`) consistent between handler and tests.

**Known follow-ups (not blockers):** Task 6 Step 7 chooses to change `rfq.Handler.Register` signature to accept a `verified fiber.Handler`; verify no other caller exists (grep confirmed sole caller is router.go). If a count query for submitted quotes already exists in `quotes.sql`, reuse it instead of adding one (Task 9).
