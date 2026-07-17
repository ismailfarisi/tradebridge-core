# Sprint 7 · Plan 5 — Targeted Polish + E2E #7 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.
> Repos: `/home/deploy/b2b/tradebridge-core/web` (Tasks 1–2), `/home/deploy/b2b/tradebridge-core` (Task 3, Go).
> Commit per task.

**Goal:** A targeted a11y + responsive polish on the core flows (visible keyboard focus everywhere; the
marketplace + core pages reflow on mobile; no dead-end copy), then **E2E #7** proving a fresh seller reaches
"first live quote" unaided (activation 100% + milestone events recorded).

**Architecture:** Task 1 adds global `:focus-visible` outlines (buttons/links/inputs) — currently only inputs
have a weak focus and buttons/links have none — and neutralizes one buyer-framed empty-state copy. Task 2
adds the repo's first media queries (0 today): the marketplace facet grid (`220px 1fr`) stacks on narrow
screens. Task 3 is the spanning Go E2E.

**Tech Stack:** CSS (globals.css), Next.js; Go/Fiber integration test.

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never push**.
- Web verify from `web/`: `npm test` (green), `npx tsc --noEmit`, bare `npm run build` (never piped).
- E2E verify: `export PATH="$HOME/.local/bin:$PATH"; export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#'); go test ./test/integration/ -run TestE2E07 -v` then full `go test ./... -p 1`.
- Polish is scoped to the CORE flows (signup → first-quote / first-RFQ). No full design-system rewrite, no
  CSS framework.

## Reference (verified)

- `web/app/globals.css`: `.input:focus/.select:focus/textarea.input:focus { outline: none; border-color: var(--primary); }`
  (buttons/links/anchors have NO focus style). **Zero `@media` queries** in the file. CSS vars include
  `--primary`, `--border`, `--surface`, `--muted`, `--text`.
- `web/components/DiscoveryResults.tsx:31`: `<div style={{ display: "grid", gridTemplateColumns: "220px 1fr", gap: "1.5rem" }}>` — the marketplace facet+results layout (does not reflow).
- `web/app/(app)/orders/page.tsx`: the Plan-4 EmptyState CTA "Browse suppliers" → `/marketplace` (buyer-framed
  on a page both roles see).
- E2E harness (`test/integration`): `buildAPI`, `buildGateway`, `registerSeller` → `(token, companyID,
  secret)` (also sets the supplier profile: description "cartons and pallets", categories ["packaging"]),
  `doJSON(t, app, method, path, token, body)`, `uniqueEmail`, `verifyUser`, `poolOrSkip`, `sqlc.New(pool)`.
- Endpoints: `POST /v1/catalog` `{name,category,summary}`; gateway `POST /agent/v1/heartbeat` (Bearer =
  agent secret); gateway `POST /agent/v1/commands` `{type:"quote.submit", idempotency_key, rfq_id, payload}`.
- `q.SetCompanyVerified(ctx, {ID, VerifiedDomain *string})` sets `companies.verification_status='verified'`.
- `q.CountActivationEventsFor(ctx, {CompanyID, Event}) (int64, error)` (added in Plan 1).
- Activation `verified` STEP reads `company.VerificationStatus`; seeding via `SetCompanyVerified` makes the
  step done but does NOT emit the `verified` EVENT (that emit only fires through the trust handler). So the
  E2E asserts the FIVE handler-emitted events (signup, profile_set, catalog_added, agent_connected,
  first_quote), and 100% completion for all five steps.

## File Structure

- Modify: `web/app/globals.css` (focus-visible + one media query)
- Modify: `web/components/DiscoveryResults.tsx` (use the responsive grid class)
- Modify: `web/app/(app)/orders/page.tsx` (role-neutral empty copy)
- Create: `test/integration/e2e_07_activation_test.go`

---

### Task 1: Keyboard-focus a11y + copy fix

**Files:** `web/app/globals.css`, `web/app/(app)/orders/page.tsx`.

- [ ] **Step 1: Add global `:focus-visible` outlines** to `web/app/globals.css` (append near the input-focus
  block). Mouse clicks stay clean (`:focus-visible` only shows for keyboard):
```css
/* Keyboard focus visibility (a11y). :focus-visible keeps mouse clicks clean. */
a:focus-visible,
button:focus-visible,
.btn:focus-visible,
[role="button"]:focus-visible,
[tabindex]:focus-visible,
summary:focus-visible {
  outline: 2px solid var(--primary);
  outline-offset: 2px;
  border-radius: 6px;
}
.input:focus-visible,
.select:focus-visible,
textarea.input:focus-visible {
  outline: 2px solid var(--primary);
  outline-offset: 1px;
}
```
  (Leave the existing `.input:focus { outline: none; border-color: var(--primary); }` — the added
  `:focus-visible` rule provides the keyboard ring; mouse focus keeps the subtle border-color.)

- [ ] **Step 2: Role-neutral orders empty copy.** In `web/app/(app)/orders/page.tsx`, change the Plan-4
  EmptyState so it reads for both buyers and sellers:
```tsx
                <EmptyState
                  title="No orders yet"
                  body="Accepted quotes become purchase orders and show up here."
                  cta={{ label: "Go to dashboard", href: "/dashboard" }}
                />
```

- [ ] **Step 3: Verify** `npx tsc --noEmit` clean; `npm run build` exits 0 (never piped). Manually confirm
  (describe in report) that tabbing to a button shows a visible outline.
- [ ] **Step 4: Commit** — `feat(web): keyboard-focus a11y outlines + role-neutral orders empty state`.

---

### Task 2: Responsive marketplace layout

**Files:** `web/app/globals.css`, `web/components/DiscoveryResults.tsx`.

- [ ] **Step 1: Add a responsive grid class** to `web/app/globals.css`:
```css
/* Marketplace search: facet sidebar beside results, stacked on narrow screens. */
.discovery-layout {
  display: grid;
  grid-template-columns: 220px 1fr;
  gap: 1.5rem;
}
@media (max-width: 640px) {
  .discovery-layout {
    grid-template-columns: 1fr;
  }
}
```

- [ ] **Step 2: Use the class** in `web/components/DiscoveryResults.tsx` — replace the inline grid style:
```tsx
    <div className="discovery-layout">
```
  (Remove the inline `style={{ display: "grid", gridTemplateColumns: "220px 1fr", gap: "1.5rem" }}` from that
  wrapper — the class now owns it.)

- [ ] **Step 3: Verify** `npx tsc --noEmit` clean; `npm run build` exits 0 (never piped). Confirm the
  marketplace renders single-column under 640px (describe in report — e.g. via the built CSS containing the
  media query).
- [ ] **Step 4: Commit** — `feat(web): responsive marketplace layout (stacks on mobile)`.

---

### Task 3: E2E #7 — fresh seller signup → first live quote, unaided

**Files:** `test/integration/e2e_07_activation_test.go`.

- [ ] **Step 1: Write the spanning E2E:**
```go
package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// stepsDone maps the activation steps array to key→done.
func stepsDone(t *testing.T, resp map[string]any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, s := range resp["steps"].([]any) {
		m := s.(map[string]any)
		out[m["key"].(string)] = m["done"].(bool)
	}
	return out
}

// E2E #7 (Sprint 7): a brand-new seller walks the whole activation path —
// profile → catalog → verified → agent connected → first quote — reaching 100%
// with the milestone events recorded, no manual help.
func TestE2E07_SellerActivationToFirstQuote(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	// registerSeller sets the supplier profile (description + ["packaging"]).
	sellerToken, sellerCompany, secret := registerSeller(t, api, "E2E7Pack")

	// Initially: profile done (registerSeller set it), the rest todo, not 100%.
	st, act := doJSON(t, api, "GET", "/v1/activation", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "seller", act["role"])
	require.Less(t, act["percent_complete"].(float64), float64(100))
	done := stepsDone(t, act)
	require.True(t, done["profile"], "registerSeller set the profile")
	require.False(t, done["catalog"])
	require.False(t, done["first_quote"])

	// Add a catalog item.
	st, _ = doJSON(t, api, "POST", "/v1/catalog", sellerToken, map[string]any{
		"name": "Cartons 30cm", "category": "packaging", "summary": "sturdy shipping box",
	})
	require.Equal(t, http.StatusCreated, st)

	// Verify the company (seed via the query layer — bypasses the DNS handler,
	// so the `verified` STEP goes done but no `verified` EVENT is emitted).
	domain := "e2e7pack.example"
	require.NoError(t, q.SetCompanyVerified(ctx, sqlc.SetCompanyVerifiedParams{ID: sellerCompany, VerifiedDomain: &domain}))

	// Agent heartbeat (Bearer = agent secret) → agent_connected.
	st, _ = doJSON(t, gw, "POST", "/agent/v1/heartbeat", secret, nil)
	require.Equal(t, http.StatusOK, st)

	// A buyer posts a matching RFQ; the seller's agent submits a quote → first_quote.
	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "E2E7 Buyer", "company_type": "buyer",
		"email": uniqueEmail("e2e7-buyer-"), "password": "password123",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)
	verifyUser(t, pool, buyer["user_id"].(string))

	st, created := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons", "text": "Need 500 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "quote.submit", "idempotency_key": "e2e7-submit", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1000, "currency": "USD", "delivery_days": 14, "notes": "in stock"},
	})
	require.Equal(t, http.StatusCreated, st)

	// Now the seller is fully activated: 100%, all five steps done.
	st, act2 := doJSON(t, api, "GET", "/v1/activation", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, float64(100), act2["percent_complete"].(float64))
	done2 := stepsDone(t, act2)
	for _, k := range []string{"profile", "catalog", "verified", "agent_connected", "first_quote"} {
		require.True(t, done2[k], "step %s should be done", k)
	}

	// The handler-emitted milestone events were recorded (verified is seeded via
	// the query layer, so its event is intentionally not asserted).
	for _, ev := range []string{"signup", "profile_set", "catalog_added", "agent_connected", "first_quote"} {
		n, err := q.CountActivationEventsFor(ctx, sqlc.CountActivationEventsForParams{CompanyID: sellerCompany, Event: ev})
		require.NoError(t, err)
		require.Equal(t, int64(1), n, "exactly one %s event", ev)
	}
}
```
> Confirm `SetCompanyVerifiedParams` field names (`ID`, `VerifiedDomain *string`) and
> `CountActivationEventsForParams` (`CompanyID`, `Event`) against the generated sqlc. If `registerSeller`'s
> tuple order differs, match it. The gateway secret is passed as the `token` arg to `doJSON` (Bearer), same
> as the existing `quote.submit` calls in `e2e_05`/`e2e_06`.

- [ ] **Step 2: Run** the E2E + full suite:
```bash
export PATH="$HOME/.local/bin:$PATH"
export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')
go test ./test/integration/ -run TestE2E07 -v
go test ./... -p 1
```
Expected: E2E passes; full suite green.

- [ ] **Step 3: Commit** — `test(core): E2E #7 — seller signup → activation 100% → first live quote`.

---

## Self-Review checklist

- Keyboard focus is visible on buttons/links/inputs across the core flows (`:focus-visible` outlines added);
  mouse clicks stay clean.
- The marketplace facet+results layout stacks to one column under 640px (first media query in the repo); no
  horizontal body scroll on mobile widths.
- No dead-end empty-state copy (orders copy is role-neutral with a working CTA).
- E2E #7 proves a fresh seller reaches activation 100% (all 5 steps) via the real HTTP API, and the five
  handler-emitted milestone events are each recorded exactly once.
- `tsc --noEmit` clean; `npm run build` exits 0 (never piped); full Go suite green under `-p 1`; nothing
  pushed.
