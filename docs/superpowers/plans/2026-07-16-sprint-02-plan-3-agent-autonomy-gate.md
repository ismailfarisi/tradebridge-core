# Sprint 2 — Plan 3: Agent-Autonomy Gate (cross-repo) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make agent autonomy (auto-quoting) a real Pro-gated feature. Core exposes the calling agent's company entitlements on the gateway; the Bun agent reads that and **fails closed** at the auto-send decision — a Free (or unreachable) company's quotes are always queued for human review, never auto-sent. Lands **E2E #2** (Free hits the autonomy gate → upgrades → autonomy unlocks, webhook-confirmed).

**Architecture:** One new read-only gateway endpoint in Core (`GET /agent/v1/entitlements`) resolves the authenticated agent's company → `entitlements.For(company)` → `{agent_autonomy, plan}`. The agent gains a tiny client (`src/core/entitlements.ts`) with a short TTL cache and fail-closed semantics, plus a combiner (`applyAutonomyEntitlement`) that downgrades an auto-send policy decision to "queued" when the company isn't entitled. Nothing about the existing policy engine changes — the entitlement is an additional gate layered on top of it.

**Tech Stack:** Core: Go 1.26.4, Fiber v2, sqlc (builds on Plan 1 `internal/entitlements` + Plan 2 `internal/billing`). Agent: Bun + TypeScript + Elysia; `bun test` with `Bun.serve` mock servers.

**Repos:** Task 1 = `tradebridge-core` (branch `sprint-02-plan3-agent-gate`). Tasks 2–3 = `tradebridge-agent` (branch `sprint-02-plan3-agent-gate`). Each repo merges locally at the end; never push.

> **Deferred follow-ups (whole-branch review — all Minor, none block merge; contract confirmed exact match, no fail-open):**
> - Core: the gateway entitlements handler calls `For()`+`PlanCode()` separately → up to 2× `GetActiveSubscription`/request (low-traffic, agent caches 60s). Resolve once if optimizing.
> - Agent: add a regression test for a truthy-but-non-boolean `agent_autonomy` (`"true"`/`1`/`null`) — the `typeof` guard already fails closed on these; the test locks it.
> - Agent: `fetchEntitlements`'s `fetch` has no timeout (a hanging Core stalls quote processing — never fails *open*; matches existing `commands.ts`/`heartbeat` pattern). Add a timeout as a hardening follow-up.

## Global Constraints

- **Fail closed:** the agent must auto-send ONLY when Core affirmatively returns `agent_autonomy: true`. Any error, non-200, malformed body, unreachable Core, or missing field ⇒ treat as NOT entitled ⇒ queue for review. Never fail open.
- **Read-only endpoint:** `GET /agent/v1/entitlements` mutates nothing; it authenticates the agent credential exactly like the other `/agent/v1/*` routes (`h.authenticate(c)`), and returns the caller's OWN company entitlements only (from the credential, never a client-supplied id).
- **Don't change the policy engine:** `evaluatePolicy`/`getPolicy` (`src/policy/`) stay as-is. The entitlement gate is layered after them.
- **Cache is a performance aid, not a correctness crutch:** cache only SUCCESSFUL positive/negative entitlement reads briefly (≤60s); never cache a failure (so a transient error doesn't pin the company closed longer than one request, nor pin it open).
- **Local only:** feature branch per repo, never push. Core tests use dev DB on `:5442` via `TEST_DATABASE_URL`; agent tests are self-contained (`bun test`).

## File Structure

- **Core:** `internal/gateway/gateway.go` (Handler gains `ent`, new route + handler), `cmd/gateway/main.go` (construct + pass the service), `test/integration/api_a3_test.go` (update `buildGateway`/`buildGatewayWithStore` helpers), `test/integration/api_agent_entitlements_test.go` (new test = E2E #2 core).
- **Agent:** `src/core/entitlements.ts` (client + cache), `test/entitlements-client.test.ts` (new), `src/reasoning/handler.ts` (combiner + wire both auto-send sites), `test/autonomy-entitlement.test.ts` (new).

---

## Task 1 (Core): gateway `GET /agent/v1/entitlements`

**Files:**
- Modify: `internal/gateway/gateway.go`, `cmd/gateway/main.go`, `test/integration/api_a3_test.go`
- Test: `test/integration/api_agent_entitlements_test.go`

**Interfaces:**
- Consumes: `entitlements.Service.For` + `.PlanCode` (Plan 1); `billing.Service.ApplyWebhookEvent` (Plan 2, in the test); the gateway's `h.authenticate(c) (authedAgent, error)` with `authedAgent.CompanyID`.
- Produces: `GET /agent/v1/entitlements` → `{"agent_autonomy": bool, "plan": string}`.

- [ ] **Step 1: Write the failing test**

Create `test/integration/api_agent_entitlements_test.go`:

```go
package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/billing"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// TestAgentEntitlements_ReflectsPlan is E2E #2's core: the entitlement the agent
// reads flips from Free (autonomy off) to Pro (autonomy on) once a billing
// webhook activates the company's subscription.
func TestAgentEntitlements_ReflectsPlan(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	_, companyID, secret := registerSeller(t, api, "AutonomyCo")

	// Free by default → autonomy off.
	st, ent := doJSON(t, gw, "GET", "/agent/v1/entitlements", secret, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, false, ent["agent_autonomy"])
	require.Equal(t, "free", ent["plan"])

	// A billing webhook activates Pro.
	require.NoError(t, billing.NewService(q, zerolog.Nop()).ApplyWebhookEvent(ctx, billing.WebhookEvent{
		ID: "evt_autonomy_" + companyID, CompanyID: companyID, CustomerID: "cus_" + companyID,
		SubscriptionID: "sub_" + companyID, Plan: "pro", Interval: "month", Status: "active",
	}))

	// Now autonomy is unlocked.
	st, ent = doJSON(t, gw, "GET", "/agent/v1/entitlements", secret, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, true, ent["agent_autonomy"])
	require.Equal(t, "pro", ent["plan"])

	// An unauthenticated request is rejected (credential required).
	st, _ = doJSON(t, gw, "GET", "/agent/v1/entitlements", "", nil)
	require.Equal(t, http.StatusUnauthorized, st)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export DB_URL="postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable"; TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestAgentEntitlements_ReflectsPlan -v`
Expected: FAIL — `buildGateway` won't compile yet (its signature changes below) OR the route 404s. (You will make it compile in Steps 3–5.)

- [ ] **Step 3: Add the entitlements service to the gateway Handler**

In `internal/gateway/gateway.go`:
- Add the import `"github.com/tradebridge/core/internal/entitlements"`.
- Add a field to `Handler`: `ent *entitlements.Service`.
- Add a trailing param to `NewHandler`: `..., notifier *notify.Notifier, ent *entitlements.Service) *Handler` and set `ent: ent` in the returned struct.
- In `Register`, add the route (after `heartbeat`):

```go
	app.Get("/agent/v1/entitlements", h.entitlements)
```

- Add the handler (near `heartbeat`):

```go
// entitlements returns the authenticated agent company's feature entitlements,
// so the agent can gate autonomy (auto-quoting) locally. Read-only.
func (h *Handler) entitlements(c *fiber.Ctx) error {
	agent, err := h.authenticate(c)
	if err != nil {
		return err
	}
	ent := h.ent.For(c.Context(), agent.CompanyID)
	return c.JSON(fiber.Map{
		"agent_autonomy": ent.AgentAutonomy,
		"plan":           h.ent.PlanCode(c.Context(), agent.CompanyID),
	})
}
```

- [ ] **Step 4: Wire the service in cmd/gateway/main.go**

In `cmd/gateway/main.go`: add the import `"github.com/tradebridge/core/internal/entitlements"`, construct `entSvc := entitlements.NewService(app.Queries, app.Logger)`, and pass `entSvc` as the new trailing arg to `gateway.NewHandler(...)`.

- [ ] **Step 5: Update the gateway test builders**

In `test/integration/api_a3_test.go`, both `buildGateway` and `buildGatewayWithStore` construct `gateway.NewHandler(...)`. Add the trailing arg `entitlements.NewService(sqlc.New(pool), zerolog.Nop())` to each call, and add the import `"github.com/tradebridge/core/internal/entitlements"` to that test file. (Everything else in those helpers stays.)

- [ ] **Step 6: Run the test**

Run: `TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestAgentEntitlements_ReflectsPlan -v`
Expected: PASS.

- [ ] **Step 7: Full suite + commit**

Run: `go build ./... && go vet ./... && TEST_DATABASE_URL="$DB_URL" go test ./...`
Expected: green (no other caller of `gateway.NewHandler` beyond `cmd/gateway` and the two test builders — confirm with `grep -rn "gateway.NewHandler" --include=*.go .`).

```bash
git add internal/gateway/gateway.go cmd/gateway/main.go test/integration/api_a3_test.go test/integration/api_agent_entitlements_test.go
git commit -m "feat(gateway): GET /agent/v1/entitlements (agent reads autonomy entitlement)"
```

---

## Task 2 (Agent): entitlements client with fail-closed + cache

**Repo:** `tradebridge-agent` (branch `sprint-02-plan3-agent-gate`).

**Files:**
- Create: `src/core/entitlements.ts`
- Test: `test/entitlements-client.test.ts`

**Interfaces:**
- Produces: `interface AgentEntitlements { agent_autonomy: boolean; plan: string }`; `fetchEntitlements(coreUrl, token) => Promise<AgentEntitlements | null>`; `autonomyAllowed(coreUrl, token) => Promise<boolean>`; `clearEntitlementCache(): void`.

- [ ] **Step 1: Write the failing test**

Create `test/entitlements-client.test.ts`:

```ts
import { afterAll, beforeEach, describe, expect, test } from "bun:test";
import { autonomyAllowed, clearEntitlementCache, fetchEntitlements } from "../src/core/entitlements.ts";

// A configurable mock Core gateway.
let mode: "pro" | "free" | "500" | "garbage" = "pro";
const server = Bun.serve({
  port: 0,
  fetch(req) {
    const url = new URL(req.url);
    if (url.pathname !== "/agent/v1/entitlements") return new Response("nf", { status: 404 });
    if (req.headers.get("authorization") !== "Bearer tok") return new Response("no", { status: 401 });
    if (mode === "500") return new Response("boom", { status: 500 });
    if (mode === "garbage") return new Response("{}", { headers: { "content-type": "application/json" } });
    const body = mode === "pro"
      ? { agent_autonomy: true, plan: "pro" }
      : { agent_autonomy: false, plan: "free" };
    return new Response(JSON.stringify(body), { headers: { "content-type": "application/json" } });
  },
});
const base = `http://localhost:${server.port}`;
afterAll(() => server.stop(true));
beforeEach(() => clearEntitlementCache());

describe("entitlements client", () => {
  test("pro → autonomy allowed", async () => {
    mode = "pro";
    expect(await autonomyAllowed(base, "tok")).toBe(true);
  });

  test("free → autonomy denied", async () => {
    mode = "free";
    expect(await autonomyAllowed(base, "tok")).toBe(false);
  });

  test("fails closed on 500", async () => {
    mode = "500";
    expect(await autonomyAllowed(base, "tok")).toBe(false);
    expect(await fetchEntitlements(base, "tok")).toBeNull();
  });

  test("fails closed on malformed body (missing agent_autonomy)", async () => {
    mode = "garbage";
    expect(await autonomyAllowed(base, "tok")).toBe(false);
  });

  test("fails closed on bad credential (401)", async () => {
    mode = "pro";
    expect(await autonomyAllowed(base, "wrong")).toBe(false);
  });

  test("fails closed when Core is unreachable", async () => {
    expect(await autonomyAllowed("http://127.0.0.1:1", "tok")).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test test/entitlements-client.test.ts`
Expected: FAIL — module `../src/core/entitlements.ts` not found.

- [ ] **Step 3: Write the client**

Create `src/core/entitlements.ts`:

```ts
// Client for Core's gateway entitlements endpoint. The agent uses this to gate
// autonomy (auto-quoting): it must FAIL CLOSED — any error, non-200, or
// malformed response means "not entitled" so quotes are queued for review.

export interface AgentEntitlements {
  agent_autonomy: boolean;
  plan: string;
}

interface CacheEntry {
  value: AgentEntitlements;
  expires: number;
}

const TTL_MS = 60_000;
const cache = new Map<string, CacheEntry>();

/** clearEntitlementCache drops all cached reads (used by tests). */
export function clearEntitlementCache(): void {
  cache.clear();
}

/**
 * fetchEntitlements returns the calling agent company's entitlements from Core's
 * gateway, or null on ANY failure (non-200, network error, malformed body).
 */
export async function fetchEntitlements(
  coreUrl: string,
  token: string,
): Promise<AgentEntitlements | null> {
  try {
    const res = await fetch(`${coreUrl}/agent/v1/entitlements`, {
      headers: { authorization: `Bearer ${token}` },
    });
    if (!res.ok) return null;
    const data = (await res.json()) as Partial<AgentEntitlements>;
    if (typeof data.agent_autonomy !== "boolean") return null; // malformed → fail closed
    return { agent_autonomy: data.agent_autonomy, plan: String(data.plan ?? "") };
  } catch {
    return null; // unreachable / parse error → fail closed
  }
}

/**
 * autonomyAllowed reports whether the company may auto-send quotes. Fails CLOSED:
 * any error/unreachable/unentitled ⇒ false. Successful reads are cached briefly;
 * failures are never cached (so a transient blip doesn't pin the company).
 */
export async function autonomyAllowed(coreUrl: string, token: string): Promise<boolean> {
  const key = `${coreUrl}\n${token}`;
  const now = Date.now();
  const hit = cache.get(key);
  if (hit && hit.expires > now) return hit.value.agent_autonomy;

  const ent = await fetchEntitlements(coreUrl, token);
  if (ent == null) return false; // fail closed; do not cache the failure
  cache.set(key, { value: ent, expires: now + TTL_MS });
  return ent.agent_autonomy;
}
```

- [ ] **Step 4: Run the test**

Run: `bun test test/entitlements-client.test.ts`
Expected: PASS (6 tests).

- [ ] **Step 5: Typecheck + commit**

Run: `bun run typecheck` (clean).

```bash
git add src/core/entitlements.ts test/entitlements-client.test.ts
git commit -m "feat(agent): entitlements client (fail-closed autonomy read + TTL cache)"
```

---

## Task 3 (Agent): gate the auto-send decision on the entitlement

**Repo:** `tradebridge-agent` (branch `sprint-02-plan3-agent-gate`).

**Files:**
- Modify: `src/reasoning/handler.ts`
- Test: `test/autonomy-entitlement.test.ts`

**Interfaces:**
- Consumes: `autonomyAllowed` (Task 2); `PolicyDecision` (`src/policy/`); `HandlerDeps` (has `coreUrl`, `token`).
- Produces (exported for testing): `applyAutonomyEntitlement(deps: HandlerDeps, gate: PolicyDecision) => Promise<PolicyDecision>`; `AUTONOMY_ENTITLEMENT_REASON: string`.

- [ ] **Step 1: Write the failing test**

Create `test/autonomy-entitlement.test.ts`:

```ts
import { afterAll, beforeEach, describe, expect, test } from "bun:test";
import { applyAutonomyEntitlement, AUTONOMY_ENTITLEMENT_REASON } from "../src/reasoning/handler.ts";
import { clearEntitlementCache } from "../src/core/entitlements.ts";
import type { PolicyDecision } from "../src/policy/index.ts";

let autonomy = true;
const server = Bun.serve({
  port: 0,
  fetch(req) {
    const url = new URL(req.url);
    if (url.pathname !== "/agent/v1/entitlements") return new Response("nf", { status: 404 });
    return new Response(JSON.stringify({ agent_autonomy: autonomy, plan: autonomy ? "pro" : "free" }), {
      headers: { "content-type": "application/json" },
    });
  },
});
const deps = { coreUrl: `http://localhost:${server.port}`, token: "tok" };
afterAll(() => server.stop(true));
beforeEach(() => clearEntitlementCache());

describe("applyAutonomyEntitlement", () => {
  test("an allowed decision stays allowed when the company IS entitled", async () => {
    autonomy = true;
    const out = await applyAutonomyEntitlement(deps, { allow: true, reasons: [] });
    expect(out.allow).toBe(true);
  });

  test("an allowed decision is DOWNGRADED to queued when NOT entitled", async () => {
    autonomy = false;
    const out = await applyAutonomyEntitlement(deps, { allow: true, reasons: [] });
    expect(out.allow).toBe(false);
    expect(out.reasons).toContain(AUTONOMY_ENTITLEMENT_REASON);
  });

  test("an already-queued decision is returned unchanged (no Core call needed)", async () => {
    const original: PolicyDecision = { allow: false, reasons: ["manual review mode"] };
    const out = await applyAutonomyEntitlement(deps, original);
    expect(out).toEqual(original);
  });

  test("fails closed → queued when Core is unreachable", async () => {
    const out = await applyAutonomyEntitlement({ coreUrl: "http://127.0.0.1:1", token: "t" }, { allow: true, reasons: [] });
    expect(out.allow).toBe(false);
    expect(out.reasons).toContain(AUTONOMY_ENTITLEMENT_REASON);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test test/autonomy-entitlement.test.ts`
Expected: FAIL — `applyAutonomyEntitlement`/`AUTONOMY_ENTITLEMENT_REASON` not exported from `handler.ts`.

- [ ] **Step 3: Add the combiner + wire both auto-send sites**

In `src/reasoning/handler.ts`:
- Add imports near the top:

```ts
import { autonomyAllowed } from "../core/entitlements.ts";
import type { PolicyDecision } from "../policy/index.ts";
```

- Add the combiner (exported), near the other top-level helpers:

```ts
export const AUTONOMY_ENTITLEMENT_REASON =
  "agent autonomy is a Pro feature — quote queued for review";

/**
 * applyAutonomyEntitlement downgrades an auto-send policy decision to "queued"
 * when the company is not entitled to agent autonomy. Fails CLOSED: if Core is
 * unreachable or the company isn't Pro, the quote is queued for human review.
 * An already-queued decision is returned unchanged (no Core round-trip).
 */
export async function applyAutonomyEntitlement(
  deps: { coreUrl: string; token: string },
  gate: PolicyDecision,
): Promise<PolicyDecision> {
  if (!gate.allow) return gate;
  if (await autonomyAllowed(deps.coreUrl, deps.token)) return gate;
  return { allow: false, reasons: [...gate.reasons, AUTONOMY_ENTITLEMENT_REASON] };
}
```

- Wire BOTH auto-send sites. Find the two occurrences of:

```ts
    const gate = evaluatePolicy(policy, decision.quote, requirements.category);
```

(one in `handleRfqMatched` ~line 92, one in the `action === "quote"` path ~line 284) and change EACH to:

```ts
    const gate = await applyAutonomyEntitlement(
      deps,
      evaluatePolicy(policy, decision.quote, requirements.category),
    );
```

Leave the surrounding `if (!gate.allow) { queue... } else { sendCommand... }` logic untouched — it already does the right thing with the (possibly downgraded) `gate`.

Note: in the `action === "quote"` path the policy is read as `getPolicy(row.sellerId)` and the decision object may be named differently — match the existing variable names at that site; only the RHS wrapping changes.

- [ ] **Step 4: Run the test + the affected suites**

Run:
```bash
bun test test/autonomy-entitlement.test.ts
bun run typecheck
bun test
```
Expected: the new test passes; typecheck clean; the FULL suite stays green (the two wired sites are covered by existing reasoning tests — confirm none regress; if a pre-existing reasoning test asserts auto-send with no mock Core, it will now fail closed to queued — if so, that test must set up a mock Core returning `agent_autonomy:true`, or the change surfaces that those tests don't exercise the gate; report any such interaction rather than weakening the gate).

- [ ] **Step 5: Commit**

```bash
git add src/reasoning/handler.ts test/autonomy-entitlement.test.ts
git commit -m "feat(agent): gate auto-send on the autonomy entitlement (fail-closed)"
```

---

## E2E #2 (satisfied by this plan)

E2E #2 = "Free user hits the autonomy gate → upgrades via Checkout → entitlement unlocks (webhook-confirmed)". It is proven across the two automatable halves:
- **Core (Task 1 test):** the entitlement the agent reads is `agent_autonomy:false / plan:free` for a fresh company and flips to `true / pro` after a billing webhook activates the subscription — the exact signal the agent consumes.
- **Agent (Task 3 test):** an auto-send-eligible quote is queued (not sent) when the company is not entitled, and sent when it is — fail-closed on unreachable Core.

A single cross-process test spanning both running binaries is out of scope for the per-repo suites (documented here so the coverage gap is explicit, not silent); the two halves above exercise every link in the chain.

## Self-Review (completed by plan author)

- **Spec coverage:** server-side gate on agent autonomy, fail-closed for Free ✓ (Task 1 endpoint + Task 3 gate); the entitlement reaches the agent via the gateway ✓ (Task 1 + Task 2); E2E #2 ✓ (two halves above). Both-role companies inherit correctly (entitlement is company-keyed; Task 1 resolves the credential's company).
- **Placeholder scan:** all steps carry complete code. The only "match the existing variable names" instruction is at the second wire site (Task 3 Step 3), where the surrounding code differs — flagged explicitly with the reason.
- **Type consistency:** `AgentEntitlements`, `fetchEntitlements`/`autonomyAllowed`/`clearEntitlementCache`, `applyAutonomyEntitlement`/`AUTONOMY_ENTITLEMENT_REASON`, and the Go `{agent_autonomy, plan}` shape are consistent across the client, the gate, and the endpoint. The Go handler returns `agent_autonomy` (bool) + `plan` (string); the TS client reads exactly those.
- **Fail-closed integrity:** every agent-side failure path (non-200, network, malformed, missing field, unreachable) returns not-entitled ⇒ queued; the cache never stores a failure. The Core endpoint is read-only and credential-scoped.
- **Cross-repo hygiene:** Task 1 lands + merges in `tradebridge-core`; Tasks 2–3 land + merge in `tradebridge-agent`; both local-only. No shared build; the contract between them is the JSON shape, covered on both sides.
