# Sprint 1 — Notifications & Comms Foundation (Design)

**Date:** 2026-07-16
**Sprint source:** `/home/deploy/b2b/sprints/sprint-01-notifications-comms.md`
**Repos:** `tradebridge-core` (Go), `tradebridge-agent` (Bun), both `web/` (Next.js), a new `@tradebridge/ui` package.
**Status:** approved for planning.

## Goal

Give the platform a voice: transactional email + in-app notifications wired to the
events that already fire on the live loop (`rfq.matched`, `quote.submitted`,
`order.created`), email verification on signup, a waitlist landing page, and the
minimal engineering backbone (CI test-on-push + Sentry) so the following sprints
run on a safety net.

## Locked decisions (from brainstorming, 2026-07-16)

1. **One spec for the whole sprint** (all 5 tracks).
2. **Delivery = Approach A: direct in-process `Notifier` injection.** Inject a
   `Notifier` into the rfq/quotes services and the gateway; call it at the same
   sites that already publish to the eventbus. In-app rows are synchronous
   (durable, testable); email is async (goroutine, own timeout). Rejected: a
   Redis subscriber (misses `quote.submitted`, which is a gateway command not a
   published domain event) and a transactional outbox (over-engineered for the
   volume).
3. **Email verification = soft gate.** Unverified users can log in but see a
   persistent banner and are blocked from the primary buyer write — **creating an
   RFQ** — via a `RequireVerified` middleware returning HTTP 403 with a
   `verify_email` code. Existing users are grandfathered as verified. Quote
   submission is agent-credential-gated (separate realm), so it is out of this gate.
4. **Waitlist = Core DB table** + public `POST /v1/waitlist`; S10 invites the list.
5. **Shared UI = publishable `@tradebridge/ui` package** (targets GitHub
   Packages). Interim consumption via a packed tarball until the package repo +
   `gh` Packages auth exist.
6. **Both repos have GitHub remotes** (`github.com/ismailfarisi/tradebridge-core`,
   `.../tradebridge-agent`) — CI-on-push runs immediately. **Sentry DSN is the
   only external secret still required.**
7. **Email provider = Resend**, behind a `Sender` interface; `LogSender` is the
   default when `RESEND_API_KEY` is unset (dev/CI never break without a provider).

## Architecture overview

```
domain action ──> eventbus.Publish (existing, unchanged: durable + Redis)
             └──> Notifier.NotifyCompany(companyID, Event)   [new]
                     ├── in-app: one notifications row per company user (sync)
                     └── email:  async goroutine ── Sender ── Resend | Log
```

`Notifier` depends only on `*sqlc.Queries` + a `Sender`, so it is usable from both
the API binary (`cmd/api`) and the gateway binary (`cmd/gateway`). Domain event
payloads carry **company** IDs, never user IDs, so a notification fans out to every
user of the target company (`ListUsersByCompany`).

## Data model (migration `014_notifications`)

```sql
-- Per-user in-app inbox. Company-scoped events fan out to one row per user.
CREATE TABLE notifications (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    user_id    TEXT NOT NULL REFERENCES users(id),
    company_id TEXT NOT NULL REFERENCES companies(id),
    type       TEXT NOT NULL,     -- rfq.matched | quote.submitted | order.created
    title      TEXT NOT NULL,
    body       TEXT NOT NULL,
    data       JSONB,             -- deep-link ids + cta
    read_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_notifications_user ON notifications (user_id, created_at DESC);
CREATE INDEX idx_notifications_user_unread ON notifications (user_id) WHERE read_at IS NULL;

-- Explicit per-user, per-event overrides. Missing row = default (both on).
CREATE TABLE notification_preferences (
    user_id    TEXT NOT NULL REFERENCES users(id),
    event_type TEXT NOT NULL,
    email      BOOLEAN NOT NULL DEFAULT TRUE,
    in_app     BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, event_type)
);

-- Email verification. Existing rows grandfathered so the soft gate locks no one out.
ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;
UPDATE users SET email_verified_at = NOW() WHERE email_verified_at IS NULL;
CREATE TABLE email_verifications (
    id, user_id, token UNIQUE, expires_at, consumed_at, created_at
);
```

Waitlist ships in a **separate migration `015_waitlist`**:
```sql
CREATE TABLE waitlist_signups (
    id, email TEXT NOT NULL UNIQUE, source TEXT, created_at
);
```

## Components & interfaces

### `internal/notify` (Core)
- `Event{Type, Title, Body, Data map[string]any, CTALabel, CTAPath}`.
- `Notifier.NotifyCompany(ctx, companyID string, ev Event)` — never returns an
  error (best-effort side effect, like the Redis publish it rides alongside).
- `Notifier.SendTransactional(to, subject, html, text)` — direct single-address
  send (verification email), no fan-out, no preference check.
- `Sender interface { Send(ctx, Email) error }` with `ResendSender` + `LogSender`;
  `NewSender(resendAPIKey, from, logger)` chooses the adapter.
- Event-type constants `TypeRFQMatched | TypeQuoteSubmitted | TypeOrderCreated |
  TypeEmailVerify` (first registry of these strings; today they are inline literals).
- `Handler`: `GET /v1/notifications` (list + `unread` count, `limit`/`offset`),
  `POST /v1/notifications/:id/read`, `POST /v1/notifications/read-all`,
  `GET/PUT /v1/notifications/preferences`.

### Email verification (Core, `internal/identity`)
- On `register`: create user (unverified is implicit — `email_verified_at` NULL),
  mint a verification token, `notify.SendTransactional` the verify email, still
  issue the JWT (soft gate).
- `POST /v1/auth/verify-email {token}`: consume token, set `email_verified_at`.
- `POST /v1/auth/resend-verification` (guarded): re-mint + resend.
- `middleware.RequireVerified(q)`: 403 `{code:"verify_email"}` when the caller's
  `email_verified_at` is NULL; mounted on `POST /v1/rfqs` (and the chat→RFQ create).

### Event wiring
| Event | Site | Recipient | Copy |
|---|---|---|---|
| `rfq.matched` | `internal/rfq/service.go` (per matched seller) | seller | "New RFQ matched: {title}" |
| `order.created` | `internal/quotes/service.go` (after commit) | buyer + seller | "Order confirmed" |
| `quote.submitted` | `internal/gateway/commands.go` (after `quotes.Record`) | buyer | "{N} suppliers quoted your RFQ" |

`Notifier` is threaded through `rfq.NewService`, `quotes.NewService`,
`gateway.NewHandler`, and `identity.NewHandler` (trailing param, matching how
`pub` is already injected). Test call-sites updated; `nil` notifier = no-op.

### Waitlist (Core)
`POST /v1/waitlist {email, source?}` — public, validates + dedupes on email
(idempotent 200), light IP rate-limit. One-page landing on `switeaz.com`.

### `@tradebridge/ui` + web apps (WS6)
- Package exports `<NotificationCenter/>` (bell, unread badge, dropdown polling
  `GET /v1/notifications`, mark-read) and `<VerifyEmailBanner/>`.
- Mounted in **core/web** (buyer) and **agent/web** (seller) top nav.
- Built as a versioned package publishable to GitHub Packages; interim install
  via packed tarball (`npm pack`) referenced by `file:`.

### Engineering backbone (WS7)
- `.github/workflows/ci.yml` in both repos: Core = `go build ./... && go test ./...`
  with a Postgres service + `TEST_DATABASE_URL`; agent = `bun install && bun test`.
- Sentry: `sentry-go` (Core), `@sentry/bun` (agent), `@sentry/nextjs` (both webs),
  initialized from `SENTRY_DSN` and **no-op when unset**.

## Config additions (Core)
`RESEND_API_KEY=""`, `EMAIL_FROM="TradeBridge <no-reply@switeaz.com>"`,
`APP_BASE_URL="http://localhost:3010"`, `SENTRY_DSN=""`. Threaded through
`httpapi.Config` + `cmd/api/main.go` + `cmd/gateway/main.go`.

## Testing strategy (TDD, red→green per unit)
- **Unit (pure, no DB):** template rendering (HTML escaping + prefs footer + CTA),
  event-type validation, the channel-defaulting rule (missing pref → both on) via
  a small pure helper, and a fake-`Sender` check that async dispatch calls it.
  `sqlc.Queries` is a concrete struct, so `Notifier`'s DB fan-out is exercised in
  integration (matching this codebase's convention — pure logic is unit-tested,
  DB-backed logic uses the `poolOrSkip` harness).
- **Integration (test DB):** `Notifier.NotifyCompany` fan-out + preference honoring
  with a fake `Sender`, notifications API (list/unread/mark-read/prefs),
  verify-email happy path + expired/consumed token, soft gate 403→verify→200 on
  RFQ create, waitlist dedupe, and each event→notification wiring
  (`rfq.matched`/`order.created`/`quote.submitted` produce the right rows).
- **E2E #1:** signup → verify email → create RFQ → in-app + (logged) email
  notification received. First test in the per-sprint suite.

## Sequencing (build order)
1. Migrations `014`+`015`, queries, `sqlc generate`.
2. `internal/notify` (Sender, Notifier, templates, handler) + unit tests.
3. Config + wiring (`httpapi`, `cmd/*`), register notify routes.
4. Email verification + soft gate.
5. Event wiring (rfq, quotes, gateway).
6. Waitlist (table + endpoint + landing).
7. `@tradebridge/ui` + notification center in both webs.
8. CI workflows + Sentry init.
9. E2E #1; green suite.

## Out of scope / provisioning (work continues around these)
- **Sentry DSN(s)** — Sentry compiled-in but dormant until provided.
- **Resend API key + SPF/DKIM/DMARC DNS** — email logged, not sent, until set.
  DNS record values documented as a runbook; applied at the registrar by the user.
- **GitHub Packages auth for `@tradebridge/ui`** — tarball install until wired.
- Full CI deploy gates / previews / status page — stay in Sprint 10.
- Agent-side notifications beyond email (the agent already gets events via the
  gateway stream) — not re-notified here.

## Definition of Done
A new user must verify their email to create an RFQ (soft gate + banner). When a
quote is submitted on a real RFQ the buyer gets an in-app notification (and a
logged/real email); same for match and order events. Users can tune preferences
per event. CI runs tests on every push in both repos. A thrown error surfaces in
Sentry once a DSN is set. The waitlist collects emails. E2E #1 is green.
