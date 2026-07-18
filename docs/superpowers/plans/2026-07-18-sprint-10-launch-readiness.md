# Sprint 10 — Launch Readiness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the code-buildable launch-readiness slice — platform business analytics (deferred S8), the waitlist-invite flow, an in-app feedback/NPS loop, SEO/OG basics, a capstone full-journey E2E, and a Prometheus `/metrics` endpoint.

**Architecture:** New Core packages `internal/analytics` (admin-realm business metrics, on-the-fly SQL), `internal/feedback` (tenant feedback + admin read), plus waitlist-invite additions to `internal/waitlist`, and a dependency-free metrics middleware. All admin surfaces mount under the S8 `/admin/v1/*` `PlatformAdminGuard`. Web adds admin analytics/waitlist/feedback pages, a feedback widget, and SEO/OG/sitemap/robots.

**Tech Stack:** Go 1.x / Fiber v2 / pgx v5 / sqlc / golang-migrate; Next.js 16 / React 19 / TS strict / vitest.

## Global Constraints

- Admin analytics/waitlist/feedback-read routes mount under the existing `/admin/v1/*` `PlatformAdminGuard`. The feedback WRITE route is tenant-authed under `/v1/*` (S8 `guard` slice). `/metrics` is unauthenticated exposition with NO tenant data in labels.
- Analytics is read-only on-the-fly SQL over existing tables — no rollup tables, no external engine.
- **MRR honesty:** `plans` has no price today. Add nullable `plans.price_cents_month`/`price_cents_year` (migration 030); MRR = Σ(active/trialing subscription × its plan monthly price; year÷12) computed ONLY when every active plan has a price, else return MRR `null` + `prices_set:false`. Never fabricate MRR.
- Feedback: `kind` ∈ {`feedback`,`nps`}; `nps` requires integer `score` 0–10; `feedback` requires non-empty `message`. Tenant-scoped writes.
- Migrations continue at **030** (plan prices), **031** (waitlist invited_at), **032** (feedback table).
- Regenerate sqlc with `sqlc generate` after any `pkg/db/query/*.sql` change (no `make`); never hand-edit `pkg/db/sqlc/*`. Tests → `tradebridge_core_test` (NEVER live), full Go suite `-p 1`. Web vitest node-env `lib/**` only. NEVER pipe `npm run build` through head/tail.
- Merge to `master` **locally, never pushed**. Commit trailers on every commit:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01JrKi3VYFWQJPRvZGwScMqh`.

## File Structure

- `pkg/db/migrations/030_plan_prices.{up,down}.sql`, `031_waitlist_invited.{up,down}.sql`, `032_feedback.{up,down}.sql`
- `pkg/db/query/analytics.sql`, `pkg/db/query/waitlist.sql` (extend), `pkg/db/query/feedback.sql`
- `internal/analytics/{service.go,handler.go,service_test.go}`
- `internal/waitlist/admin.go` (+ handler test)
- `internal/feedback/{handler.go,handler_test.go}`
- `pkg/middleware/metrics.go` (+ test)
- `internal/httpapi/router.go` (wire all)
- Web: `web/app/admin/{analytics,waitlist,feedback}/page.tsx`; `web/lib/{analytics,waitlistAdmin,feedbackAdmin}.ts` (admin clients); `web/lib/feedback.ts` + `web/lib/feedback.test.ts` (nps helpers); `web/components/FeedbackWidget.tsx`; `web/components/AdminShell.tsx` (nav); `web/app/layout.tsx` (OG metadata); `web/app/sitemap.ts`, `web/app/robots.ts`; `web/lib/track.ts` + test.
- `test/integration/e2e_10_full_journey_test.go`; `scripts/smoke.sh`

---

### Task 1: Business analytics (migration 030 + `internal/analytics` + admin web)

**Files:**
- Create: `pkg/db/migrations/030_plan_prices.{up,down}.sql`, `pkg/db/query/analytics.sql`, `internal/analytics/{service.go,handler.go,service_test.go}`
- Modify: `internal/httpapi/router.go`; web `AdminShell.tsx` + `web/lib/analytics.ts` + `web/app/admin/analytics/page.tsx`

**Interfaces:**
- Produces: `analytics.NewService(q *sqlc.Queries) *Service`; `(*Service).Business(ctx) (Summary, error)`; `analytics.NewHandler(svc).Register(app, adminGuard)` → `GET /admin/v1/analytics/business`.

- [ ] **Step 1: Migration 030** `030_plan_prices.up.sql`:
```sql
ALTER TABLE plans ADD COLUMN price_cents_month INTEGER;
ALTER TABLE plans ADD COLUMN price_cents_year INTEGER;
```
`.down.sql`:
```sql
ALTER TABLE plans DROP COLUMN IF EXISTS price_cents_year;
ALTER TABLE plans DROP COLUMN IF EXISTS price_cents_month;
```

- [ ] **Step 2: Queries** `pkg/db/query/analytics.sql`. CONFIRM the exact `orders.status` cancelled literal and column names against `pkg/db/migrations/007_quotes_orders.up.sql`/`019_order_lifecycle.up.sql` before finalizing (the seller-analytics revenue query already excludes cancelled — mirror its literal):
```sql
-- name: AnalyticsGMV :one
SELECT COALESCE(SUM(total_cents), 0)::bigint AS gmv_cents,
       COUNT(*)::bigint AS orders
FROM orders
WHERE status <> 'cancelled';

-- name: AnalyticsGMVByWeek :many
SELECT date_trunc('week', created_at)::date AS week,
       COALESCE(SUM(total_cents), 0)::bigint AS gmv_cents
FROM orders
WHERE status <> 'cancelled' AND created_at > NOW() - INTERVAL '12 weeks'
GROUP BY 1 ORDER BY 1;

-- name: AnalyticsActiveCompanies :one
SELECT
  (SELECT COUNT(DISTINCT buyer_company_id) FROM rfqs  WHERE created_at > NOW() - INTERVAL '30 days')::bigint AS active_buyers,
  (SELECT COUNT(DISTINCT seller_company_id) FROM quotes WHERE created_at > NOW() - INTERVAL '30 days')::bigint AS active_sellers;

-- name: AnalyticsFunnel :many
SELECT event, COUNT(DISTINCT company_id)::bigint AS companies
FROM activation_events GROUP BY event ORDER BY event;

-- name: AnalyticsSubscriptionMix :many
SELECT s.plan, s.status, COUNT(*)::bigint AS n,
       p.price_cents_month, p.price_cents_year
FROM subscriptions s JOIN plans p ON p.code = s.plan
GROUP BY s.plan, s.status, p.price_cents_month, p.price_cents_year;
```

- [ ] **Step 3: Regenerate sqlc** — `sqlc generate`. Confirm generated row types (some aggregates may type as `interface{}`/`int64`; handle with the generated types).

- [ ] **Step 4: Failing service test** `internal/analytics/service_test.go` (DB harness like `internal/adminconsole/handler_test.go`): seed an order (non-cancelled) + an activation event + a subscription; assert `Business()` returns GMV ≥ the order total, the funnel contains the seeded event, and `MRR` is `nil` with `PricesSet=false` when the plan has no price (then set a price and assert MRR computes).

- [ ] **Step 5: Implement service** `internal/analytics/service.go`:
```go
// Package analytics computes platform-wide business metrics on-the-fly (admin realm).
package analytics

import (
	"context"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

type Service struct{ q *sqlc.Queries }

func NewService(q *sqlc.Queries) *Service { return &Service{q: q} }

type Summary struct {
	GMVCents      int64            `json:"gmv_cents"`
	Orders        int64            `json:"orders"`
	GMVByWeek     []WeekPoint      `json:"gmv_by_week"`
	ActiveBuyers  int64            `json:"active_buyers"`
	ActiveSellers int64            `json:"active_sellers"`
	Funnel        []FunnelStep     `json:"funnel"`
	PlanMix       []PlanMixRow     `json:"plan_mix"`
	MRRCents      *int64           `json:"mrr_cents"`   // nil when prices unset
	PricesSet     bool             `json:"prices_set"`
	ActiveSubs    int64            `json:"active_subs"`
	CanceledSubs  int64            `json:"canceled_subs"`
	ChurnPct      float64          `json:"churn_pct"`
}
type WeekPoint struct { Week string `json:"week"`; GMVCents int64 `json:"gmv_cents"` }
type FunnelStep struct { Event string `json:"event"`; Companies int64 `json:"companies"` }
type PlanMixRow struct { Plan, Status string `json:"-"`; PlanCode string `json:"plan"`; SubStatus string `json:"status"`; Count int64 `json:"count"` }

func (s *Service) Business(ctx context.Context) (Summary, error) {
	var out Summary
	if g, err := s.q.AnalyticsGMV(ctx); err == nil {
		out.GMVCents, out.Orders = g.GmvCents, g.Orders
	}
	if w, err := s.q.AnalyticsGMVByWeek(ctx); err == nil {
		for _, r := range w {
			out.GMVByWeek = append(out.GMVByWeek, WeekPoint{Week: r.Week.Format("2006-01-02"), GMVCents: r.GmvCents})
		}
	}
	if a, err := s.q.AnalyticsActiveCompanies(ctx); err == nil {
		out.ActiveBuyers, out.ActiveSellers = a.ActiveBuyers, a.ActiveSellers
	}
	if f, err := s.q.AnalyticsFunnel(ctx); err == nil {
		for _, r := range f {
			out.Funnel = append(out.Funnel, FunnelStep{Event: r.Event, Companies: r.Companies})
		}
	}
	// Plan mix + MRR + churn.
	mix, err := s.q.AnalyticsSubscriptionMix(ctx)
	if err == nil {
		var mrr int64
		pricesSet := true
		active := func(st string) bool { return st == "active" || st == "trialing" }
		for _, r := range mix {
			out.PlanMix = append(out.PlanMix, PlanMixRow{PlanCode: r.Plan, SubStatus: r.Status, Count: r.N})
			if active(r.Status) {
				out.ActiveSubs += r.N
				switch {
				case r.PriceCentsMonth != nil:
					mrr += *r.PriceCentsMonth * r.N
				case r.PriceCentsYear != nil:
					mrr += (*r.PriceCentsYear / 12) * r.N
				default:
					pricesSet = false
				}
			}
			if r.Status == "canceled" {
				out.CanceledSubs += r.N
			}
		}
		if pricesSet && out.ActiveSubs > 0 {
			out.MRRCents = &mrr
		}
		out.PricesSet = pricesSet
		if total := out.ActiveSubs + out.CanceledSubs; total > 0 {
			out.ChurnPct = float64(out.CanceledSubs) / float64(total) * 100.0
		}
	}
	return out, nil
}
```
NOTE: adjust field access (`.GmvCents`, `.PriceCentsMonth` as `*int64` or `pgtype`) to the ACTUAL sqlc-generated types; nullable int columns generate as `*int32`/`*int64` (repo uses `emit_pointers_for_null_types`). Fix the arithmetic types accordingly.

- [ ] **Step 6: Handler** `internal/analytics/handler.go`:
```go
package analytics

import "github.com/gofiber/fiber/v2"

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(app *fiber.App, adminGuard fiber.Handler) {
	app.Get("/admin/v1/analytics/business", adminGuard, h.business)
}

func (h *Handler) business(c *fiber.Ctx) error {
	s, err := h.svc.Business(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(s)
}
```

- [ ] **Step 7: Register** in `internal/httpapi/router.go` admin block: `analytics.NewHandler(analytics.NewService(q)).Register(f, adminGuard)`. Add the import.

- [ ] **Step 8: Run tests + build** — `go build ./... && go test ./internal/analytics/`.

- [ ] **Step 9: Web** — `web/lib/analytics.ts` add `getBusinessAnalytics(token)` (typed to `Summary`) via the existing admin `req<T>`; `web/app/admin/analytics/page.tsx` (`"use client"`, in AdminShell): tiles for GMV ($), active buyers/sellers, MRR (or "prices unset" when `!prices_set`), churn %; a funnel list; a GMV-by-week table. Add `{ href: "/admin/analytics", label: "Analytics" }` to `AdminShell` NAV. Verify: `cd web && npx tsc --noEmit && npm run build`.

- [ ] **Step 10: Commit** — `feat(core+web): platform business analytics dashboard (S10)` + trailers.

---

### Task 2: Waitlist invite flow (migration 031 + admin)

**Files:**
- Create: `pkg/db/migrations/031_waitlist_invited.{up,down}.sql`, `internal/waitlist/admin.go`, `internal/waitlist/admin_test.go`
- Modify: `pkg/db/query/waitlist.sql`, `internal/httpapi/router.go`; web `web/lib/waitlistAdmin.ts` + `web/app/admin/waitlist/page.tsx` + AdminShell nav

**Interfaces:**
- Consumes: `notify.Sender` (`Send(ctx, notify.Email) error`) — the notifier already built in the router.
- Produces: `waitlist.NewAdminHandler(q, sender, appBaseURL, logger).Register(app, adminGuard)` → `GET /admin/v1/waitlist`, `POST /admin/v1/waitlist/invite`.

- [ ] **Step 1: Migration 031** `ALTER TABLE waitlist_signups ADD COLUMN invited_at TIMESTAMPTZ;` (down: DROP COLUMN IF EXISTS).

- [ ] **Step 2: Queries** — add to `pkg/db/query/waitlist.sql`:
```sql
-- name: ListWaitlist :many
SELECT * FROM waitlist_signups
WHERE (sqlc.narg(only)::text IS NULL
       OR (sqlc.narg(only) = 'pending' AND invited_at IS NULL)
       OR (sqlc.narg(only) = 'invited' AND invited_at IS NOT NULL))
ORDER BY created_at DESC
LIMIT sqlc.arg(lim);

-- name: ListPendingWaitlistEmails :many
SELECT id, email FROM waitlist_signups WHERE invited_at IS NULL ORDER BY created_at;

-- name: GetWaitlistByIDs :many
SELECT id, email FROM waitlist_signups WHERE id = ANY(sqlc.arg(ids)::text[]) AND invited_at IS NULL;

-- name: MarkWaitlistInvited :exec
UPDATE waitlist_signups SET invited_at = NOW() WHERE id = ANY(sqlc.arg(ids)::text[]);
```
`sqlc generate`.

- [ ] **Step 3: Failing handler test** `internal/waitlist/admin_test.go` (admin-guard-stub harness like `internal/adminconsole/handler_test.go`): seed 2 pending signups; `POST /admin/v1/waitlist/invite` (no ids → all pending) with a **stub/log sender** (use `notify.LogSender{}` or a counting fake) → assert response reports 2 sent, both rows now have `invited_at` set, and `GET /admin/v1/waitlist?status=invited` returns them.

- [ ] **Step 4: Implement admin handler** `internal/waitlist/admin.go`:
```go
package waitlist

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

type AdminHandler struct {
	q       *sqlc.Queries
	sender  notify.Sender
	baseURL string
	logger  zerolog.Logger
}

func NewAdminHandler(q *sqlc.Queries, sender notify.Sender, baseURL string, logger zerolog.Logger) *AdminHandler {
	return &AdminHandler{q: q, sender: sender, baseURL: baseURL, logger: logger}
}

func (h *AdminHandler) Register(app *fiber.App, adminGuard fiber.Handler) {
	g := app.Group("/admin/v1/waitlist", adminGuard)
	g.Get("/", h.list)
	g.Post("/invite", h.invite)
}

func (h *AdminHandler) list(c *fiber.Ctx) error {
	var only *string
	if s := c.Query("status"); s == "pending" || s == "invited" {
		only = &s
	}
	limit := 500
	if n, err := strconv.Atoi(c.Query("limit")); err == nil && n > 0 && n <= 2000 {
		limit = n
	}
	rows, err := h.q.ListWaitlist(c.Context(), sqlc.ListWaitlistParams{Only: only, Lim: int32(limit)})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"signups": rows, "count": len(rows)})
}

type inviteReq struct {
	IDs []string `json:"ids"`
}

func (h *AdminHandler) invite(c *fiber.Ctx) error {
	var req inviteReq
	_ = c.BodyParser(&req)
	var targets []struct{ ID, Email string }
	if len(req.IDs) == 0 {
		rows, err := h.q.ListPendingWaitlistEmails(c.Context())
		if err != nil {
			return err
		}
		for _, r := range rows {
			targets = append(targets, struct{ ID, Email string }{r.ID, r.Email})
		}
	} else {
		rows, err := h.q.GetWaitlistByIDs(c.Context(), req.IDs)
		if err != nil {
			return err
		}
		for _, r := range rows {
			targets = append(targets, struct{ ID, Email string }{r.ID, r.Email})
		}
	}
	sent, ids := 0, make([]string, 0, len(targets))
	for _, t := range targets {
		email := notify.Email{
			To:      t.Email,
			Subject: "You're invited to TradeBridge",
			HTML:    `<p>TradeBridge is open. <a href="` + h.baseURL + `/register">Create your account</a>.</p>`,
			Text:    "TradeBridge is open. Create your account: " + h.baseURL + "/register",
		}
		if err := h.sender.Send(c.Context(), email); err != nil {
			h.logger.Warn().Err(err).Str("email", t.Email).Msg("waitlist invite send failed")
			continue
		}
		sent++
		ids = append(ids, t.ID)
	}
	if len(ids) > 0 {
		if err := h.q.MarkWaitlistInvited(c.Context(), ids); err != nil {
			return err
		}
	}
	return c.JSON(fiber.Map{"sent": sent, "attempted": len(targets)})
}
```
NOTE: confirm the `notify.Email` struct field names (`To`/`Subject`/`HTML`/`Text`) against `internal/notify/sender.go`; adjust. Only rows successfully sent are marked invited (so a transient failure retries next run). Waitlist invites are NOT written to `audit_logs` (its `company_id` is NOT NULL and a waitlist signup has no company); the per-recipient failures are logged instead.

- [ ] **Step 5: Register** in router admin block: `waitlist.NewAdminHandler(q, sender, appBaseURLOr(cfg.AppBaseURL), cfg.Logger).Register(f, adminGuard)` (reuse the `sender`/`notifier` + `appBaseURLOr` helper already in the router).

- [ ] **Step 6: Run tests + build** — `go build ./... && go test ./internal/waitlist/`.

- [ ] **Step 7: Web** — `web/lib/waitlistAdmin.ts` (`listWaitlist`, `inviteWaitlist(token, ids?)`); `web/app/admin/waitlist/page.tsx` (list pending/invited, checkboxes + "Invite selected" / "Invite all pending"); AdminShell nav `{ href: "/admin/waitlist", label: "Waitlist" }`. Verify tsc + build.

- [ ] **Step 8: Commit** — `feat(core+web): waitlist invite flow (S10)` + trailers.

---

### Task 3: In-app feedback + NPS (migration 032 + Core + web)

**Files:**
- Create: `pkg/db/migrations/032_feedback.{up,down}.sql`, `pkg/db/query/feedback.sql`, `internal/feedback/{handler.go,handler_test.go}`
- Modify: `internal/httpapi/router.go`; web `web/lib/feedback.ts` + `web/lib/feedback.test.ts` + `web/lib/feedbackAdmin.ts` + `web/components/FeedbackWidget.tsx` + `web/components/AppShell.tsx` + `web/app/admin/feedback/page.tsx` + AdminShell nav

**Interfaces:**
- Produces: `POST /v1/feedback` (tenant-authed); `GET /admin/v1/feedback` (admin); web `<FeedbackWidget/>` + pure `npsCategory(score)`, `npsScore(rows)`.

- [ ] **Step 1: Migration 032** `032_feedback.up.sql`:
```sql
CREATE TABLE feedback (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id TEXT NOT NULL REFERENCES companies(id),
    user_id    TEXT NOT NULL REFERENCES users(id),
    kind       TEXT NOT NULL CHECK (kind IN ('feedback','nps')),
    score      INTEGER CHECK (score BETWEEN 0 AND 10),
    message    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_feedback_created ON feedback (created_at DESC);
```
`.down.sql`: `DROP TABLE IF EXISTS feedback;`

- [ ] **Step 2: Queries** `pkg/db/query/feedback.sql`:
```sql
-- name: CreateFeedback :one
INSERT INTO feedback (company_id, user_id, kind, score, message)
VALUES ($1, $2, $3, sqlc.narg(score), sqlc.narg(message))
RETURNING *;

-- name: ListFeedback :many
SELECT * FROM feedback ORDER BY created_at DESC LIMIT sqlc.arg(lim);
```
`sqlc generate`.

- [ ] **Step 3: Failing handler test** `internal/feedback/handler_test.go` (tenant-authed harness + a company/user seed): `POST /v1/feedback {kind:"nps",score:9}` → 201/200 + row; `{kind:"nps"}` (no score) → 400; `{kind:"feedback",message:""}` → 400; `{kind:"feedback",message:"hi"}` → ok; `{kind:"bogus"}` → 400.

- [ ] **Step 4: Implement handler** `internal/feedback/handler.go`:
```go
package feedback

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

type Handler struct{ q *sqlc.Queries }

func NewHandler(q *sqlc.Queries) *Handler { return &Handler{q: q} }

func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Post("/v1/feedback", append(append([]fiber.Handler{}, guard...), h.create)...)
}

func (h *Handler) RegisterAdmin(app *fiber.App, adminGuard fiber.Handler) {
	app.Get("/admin/v1/feedback", adminGuard, h.list)
}

type createReq struct {
	Kind    string `json:"kind"`
	Score   *int32 `json:"score"`
	Message string `json:"message"`
}

func (h *Handler) create(c *fiber.Ctx) error {
	var req createReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	switch req.Kind {
	case "nps":
		if req.Score == nil || *req.Score < 0 || *req.Score > 10 {
			return fiber.NewError(fiber.StatusBadRequest, "score 0-10 required for nps")
		}
	case "feedback":
		if req.Message == "" {
			return fiber.NewError(fiber.StatusBadRequest, "message required for feedback")
		}
	default:
		return fiber.NewError(fiber.StatusBadRequest, "kind must be feedback or nps")
	}
	var msg *string
	if req.Message != "" {
		msg = &req.Message
	}
	row, err := h.q.CreateFeedback(c.Context(), sqlc.CreateFeedbackParams{
		CompanyID: middleware.CompanyID(c),
		UserID:    middleware.UserID(c),
		Kind:      req.Kind,
		Score:     req.Score,
		Message:   msg,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(row)
}

func (h *Handler) list(c *fiber.Ctx) error {
	rows, err := h.q.ListFeedback(c.Context(), 500)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"feedback": rows, "count": len(rows)})
}
```
NOTE: confirm `CreateFeedbackParams.Score`/`Message` nullable types (`*int32`/`*string`) against generated sqlc; adjust.

- [ ] **Step 5: Register** in router: `feedback.NewHandler(q)` — `.Register(f, guard...)` (tenant) + `.RegisterAdmin(f, adminGuard)` (in the admin block).

- [ ] **Step 6: Run tests + build** — `go build ./... && go test ./internal/feedback/`.

- [ ] **Step 7: Web pure helpers** `web/lib/feedback.ts`:
```ts
"use client";
import { CORE_URL } from "./api";
export type FeedbackInput = { kind: "feedback"; message: string } | { kind: "nps"; score: number };
export async function sendFeedback(token: string, input: FeedbackInput): Promise<void> {
  const res = await fetch(`${CORE_URL}/v1/feedback`, {
    method: "POST",
    headers: { "content-type": "application/json", authorization: `Bearer ${token}` },
    body: JSON.stringify(input),
  });
  if (!res.ok) throw new Error(await res.text());
}
export type NpsCategory = "promoter" | "passive" | "detractor";
export function npsCategory(score: number): NpsCategory {
  if (score >= 9) return "promoter";
  if (score >= 7) return "passive";
  return "detractor";
}
// npsScore: % promoters − % detractors over nps rows, rounded; 0 when no rows.
export function npsScore(scores: number[]): number {
  if (scores.length === 0) return 0;
  const p = scores.filter((s) => s >= 9).length;
  const d = scores.filter((s) => s <= 6).length;
  return Math.round(((p - d) / scores.length) * 100);
}
```
`web/lib/feedback.test.ts` — vitest for `npsCategory` (10→promoter, 8→passive, 5→detractor) + `npsScore` ([10,10,0]→33, []→0).

- [ ] **Step 8: Widget + admin page** — `web/components/FeedbackWidget.tsx` (`"use client"`, uses `useToken`): a small fixed "Feedback" button → modal with two tabs ("Send feedback" textarea → `sendFeedback({kind:"feedback",message})`; "Rate us" 0–10 buttons → `sendFeedback({kind:"nps",score})`), success toast. Mount in `web/components/AppShell.tsx` (near `<main>`, authed-only). `web/lib/feedbackAdmin.ts` (`listFeedback`) + `web/app/admin/feedback/page.tsx` (table + an NPS summary using `npsScore` over `nps` rows) + AdminShell nav `{ href: "/admin/feedback", label: "Feedback" }`.

- [ ] **Step 9: Verify web** — `cd web && npx tsc --noEmit && npx vitest run && npm run build`.

- [ ] **Step 10: Commit** — `feat(core+web): in-app feedback + NPS (S10)` + trailers.

---

### Task 4: SEO / OpenGraph / sitemap / robots (web)

**Files:**
- Modify: `web/app/layout.tsx` (default OG/metadata); per-page `metadata` on public routes
- Create: `web/app/sitemap.ts`, `web/app/robots.ts`, `web/lib/track.ts` + `web/lib/track.test.ts`, a default OG image asset

**Interfaces:**
- Produces: site-wide OG defaults; `sitemap()`/`robots()`; pure `track.ts` `trackEvent(name, props, consent)`.

- [ ] **Step 1: Default metadata + OG** in `web/app/layout.tsx` — extend the existing `metadata` export with `title` (template), `description`, `metadataBase: new URL("https://switeaz.com")`, `openGraph` (title/description/url/siteName/images: [default OG]) and `twitter` (`card:"summary_large_image"`). Add a per-page `export const metadata` (title+description) to the public pages: landing (root), `pricing`, `(marketing)/legal/*`, and a `generateMetadata` on the public storefront `s/[id]` (seller name/description) and `marketplace`.

- [ ] **Step 2: `web/app/robots.ts`**:
```ts
import type { MetadataRoute } from "next";
export default function robots(): MetadataRoute.Robots {
  return {
    rules: { userAgent: "*", allow: "/", disallow: ["/admin", "/dashboard", "/account", "/rfqs", "/orders", "/chat", "/settings"] },
    sitemap: "https://switeaz.com/sitemap.xml",
  };
}
```

- [ ] **Step 3: `web/app/sitemap.ts`** — static public URLs (`/`, `/pricing`, `/marketplace`, `/legal/terms`, `/legal/privacy`, `/legal/cookies`) as `MetadataRoute.Sitemap`. (Dynamic storefront URLs optional — keep static for launch.)

- [ ] **Step 4: OG image** — add a static default OG image at `web/public/og-default.png` (a simple branded card; if generating an image is impractical, add a minimal SVG `web/public/og-default.svg` and reference it, or a Next `opengraph-image.tsx` route rendering a simple card). Reference it from the layout `openGraph.images`.

- [ ] **Step 5: Consent-gated `track.ts`** — `web/lib/track.ts`:
```ts
"use client";
import { readConsent } from "./consent";
// trackEvent no-ops unless the user has accepted cookies (S9) AND a provider is wired.
// Today it is a wired stub: it returns whether it WOULD have tracked, so it's testable
// and ready to attach a provider later.
export function trackEvent(name: string, props: Record<string, unknown> = {}, consent = readConsent()): boolean {
  if (consent !== "accepted") return false;
  // provider hook goes here (no external provider added in S10)
  return true;
}
```
`web/lib/track.test.ts` — vitest: consent "accepted" → true; "rejected"/null → false.

- [ ] **Step 6: Verify** — `cd web && npx tsc --noEmit && npx vitest run && npm run build` (confirm `/sitemap.xml` + `/robots.txt` appear in the route list).

- [ ] **Step 7: Commit** — `feat(web): SEO/OpenGraph + sitemap + robots + consent-gated track stub (S10)` + trailers.

---

### Task 5: Capstone full-journey E2E + prod smoke script (Core)

**Files:**
- Create: `test/integration/e2e_10_full_journey_test.go`, `scripts/smoke.sh`

- [ ] **Step 1: Study prior E2Es** — `test/integration/e2e_08_admin_ops_test.go` and `e2e_09_dsr_test.go` for the router build, auth-token helper, and the seed helpers (register company/user, create supplier profile + catalog, create RFQ, gateway quote submit, accept order, invoice). Ensure the test DB is at v32 (`migrate ... up`).

- [ ] **Step 2: Write the E2E** `test/integration/e2e_10_full_journey_test.go` — the full journey through the real API:
  1. Register a BUYER company + a SELLER company (each + owner). 
  2. Seller: create supplier profile + at least one catalog item.
  3. Buyer: post an RFQ that matches the seller (assert `match_count >= 1`).
  4. Seller agent: submit a quote via the gateway (`/agent/v1/commands` `quote.submit`, using an issued agent credential — reuse the gateway seed helper from earlier E2Es).
  5. Buyer: accept the quote → assert an order is created and an invoice row/number exists (auto-invoice on accept, S4).
  6. Assert the order status is accepted/fulfilled and the invoice `total_cents` matches the quote.
  Reuse the exact helpers the earlier E2Es use; do NOT invent a new harness. Match the file's build tags/naming (`TestE2E10...`, `//go:build integration` if present).

- [ ] **Step 3: Run** — `go test ./test/integration/ -run E2E10 -p 1 -v`, then `go test ./... -p 1` (green).

- [ ] **Step 4: `scripts/smoke.sh`** — a runnable prod smoke (bash) that curls, against a `BASE=${1:-https://backend.switeaz.com}` and `WEB=${2:-https://switeaz.com}`:
  - `GET $BASE/health` == 200 and body contains `"ok"`
  - `GET $BASE/v1/account/export` == 401 (guarded)
  - `GET $BASE/public/v1/discovery?q=test` == 200
  - `GET $WEB/` == 200, `GET $WEB/pricing` == 200, `GET $WEB/legal/terms` == 200
  - prints PASS/FAIL per check and exits non-zero if any FAIL.
  Make it executable (`chmod +x`). This is the "prod smoke" deliverable (rollback remains a runbook note in the commit body / this plan).

- [ ] **Step 5: Commit** — `test(core): E2E #10 full journey (register→RFQ→quote→accept→invoice) + prod smoke script (S10)` + trailers.

---

### Task 6: Prometheus `/metrics` endpoint (Core, dependency-free)

**Files:**
- Create: `pkg/middleware/metrics.go`, `pkg/middleware/metrics_test.go`
- Modify: `internal/httpapi/router.go`

**Interfaces:**
- Produces: `middleware.NewMetrics() *Metrics`; `(*Metrics).Middleware() fiber.Handler`; `(*Metrics).Handler() fiber.Handler` (serves `GET /metrics`).

- [ ] **Step 1: Failing test** `pkg/middleware/metrics_test.go`: build a fiber app with the metrics middleware + a `/x` route + the `/metrics` handler; hit `/x` twice; GET `/metrics` → body is Prometheus text containing `http_requests_total` with a count ≥ 2 and NO raw ids in labels (labels: method, route, status). Assert `/metrics` itself is not counted (or is counted under route `/metrics` — pick one and assert consistently).

- [ ] **Step 2: Implement (dependency-free)** `pkg/middleware/metrics.go` — an in-memory counter keyed by `(method, routeTemplate, statusClass)` + a duration sum/count per key (a simple summary), guarded by a mutex; `Middleware()` records `c.Route().Path` (the TEMPLATE, e.g. `/v1/rfqs/:id`, never the raw path — no ids leak) + `c.Response().StatusCode()` after `c.Next()` and the elapsed time; `Handler()` writes Prometheus text exposition:
```
# HELP http_requests_total Total HTTP requests
# TYPE http_requests_total counter
http_requests_total{method="GET",route="/v1/rfqs/:id",status="2xx"} 12
# HELP http_request_duration_seconds_sum ...
http_request_duration_seconds_sum{...} 0.34
http_request_duration_seconds_count{...} 12
```
Use `strconv`/`fmt` for the text; escape label values minimally (route templates are safe). No external dependency. Keep the map bounded by route-template cardinality (finite — it's the registered routes).

- [ ] **Step 3: Run test — passes.** `go test ./pkg/middleware/ -run Metrics`.

- [ ] **Step 4: Wire router** — construct one `metrics := middleware.NewMetrics()`, add `f.Use(metrics.Middleware())` EARLY (after the request logger, before route groups), and mount `f.Get("/metrics", metrics.Handler())` (unauthenticated, alongside `/health`).

- [ ] **Step 5: Build + full suite** — `go build ./... && go test ./... -p 1` green.

- [ ] **Step 6: Commit** — `feat(core): dependency-free Prometheus /metrics (golden signals) (S10)` + trailers.

---

## Self-Review

- **Spec coverage:** business analytics (T1) + waitlist invite (T2) + feedback/NPS (T3) + SEO/OG/sitemap/robots (T4) + capstone E2E + smoke (T5) + /metrics (T6). Deferred items (marketing site, agent-Postgres, CI/infra, feature-flag) intentionally absent. ✅
- **Type consistency:** analytics `Summary` fields consumed by the web analytics page; `npsScore`/`npsCategory` signatures match their tests + the admin feedback page; `notify.Sender.Send(ctx, Email)` used in T2; migrations 030/031/032 are sequential and each owned by one task. ✅
- **Placeholder scan:** the sqlc-generated-type NOTEs (nullable `*int32`/`*int64`, `notify.Email` fields, `orders.status` cancelled literal) are explicit "confirm against the real generated type/schema" instructions, not gaps. The MRR-honesty logic (nil when prices unset) is fully specified. ✅
- **MRR correctness:** MRR is `*int64` returning nil + `PricesSet:false` unless every active plan has a price — matches the "never fabricate MRR" constraint. ✅
