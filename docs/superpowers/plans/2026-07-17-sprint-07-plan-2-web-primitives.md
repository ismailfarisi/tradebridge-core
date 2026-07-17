# Sprint 7 · Plan 2 — Shared Web Primitives Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.
> Repo: `/home/deploy/b2b/tradebridge-core/web` (Next.js 16 / React 19 / TS strict). Commit per task.

**Goal:** The reusable activation/conversion primitives the seller & buyer onboarding plans build on:
`lib/activation.ts` (client + pure nudge helper), `<EmptyState>`, `<ActivationChecklist>`, `<UpgradeNudge>`.

**Architecture:** `lib/activation.ts` fetches `GET /v1/activation` and exposes a pure `shouldNudge` helper
(unit-tested). `<EmptyState>` is presentational. `<ActivationChecklist>` fetches activation status and
renders ordered steps + a progress bar, auto-hiding at 100%. `<UpgradeNudge>` reuses the existing
`usagePercent`/`UpgradeModal`/`getBillingSummary` and shows a near-limit banner / locked-feature CTA.

**Tech Stack:** Next.js App Router (client components), TypeScript strict, vitest (node env, mocked fetch —
**components are NOT unit-tested** in this repo's setup; testable logic lives in `lib` and is covered there;
components are verified by `tsc --noEmit` + `npm run build`).

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never push**.
- Verify from `web/`: `npm test` (vitest), `npx tsc --noEmit`, bare `npm run build` (**never piped** —
  SIGPIPE corrupts `.next`).
- Reuse existing utilities/components — do NOT add a CSS framework or new test infra. Path alias `@/*` → web
  root.
- Reuse from `lib/billing.ts`: `getBillingSummary`, `usagePercent(used,limit)`, `usageLabel(used,limit)`,
  `PaywallError`, types `Plan`/`Usage`. Reuse `components/UpgradeModal.tsx`
  (`default export UpgradeModal({feature, planRequired, onClose, onUpgrade?, busy?})`).

## Reference (verified conventions)

- Client pattern: `lib/dashboard.ts` (authed `fetch` on `CORE_URL` with `authorization: Bearer <token>`,
  `res.text()` → `JSON.parse`). `CORE_URL` from `lib/api.ts`.
- `useToken()` / `useMe()` from `lib/auth.ts`.
- `GET /v1/activation` → `{role:"seller"|"buyer", percent_complete:number,
  steps:[{key,label,done,cta_href,hint}]}`.
- `usagePercent(used,limit)` → 0 when `limit<0` (unlimited), 100 when `limit===0`, else rounded %.
- CSS utilities present: `.card .stat .badge .btn .muted .small .grid .col .row .field .input .tbl`.

## File Structure

- Create: `web/lib/activation.ts` + `web/lib/activation.test.ts`
- Create: `web/components/EmptyState.tsx`
- Create: `web/components/ActivationChecklist.tsx`
- Create: `web/components/UpgradeNudge.tsx`

---

### Task 1: `lib/activation.ts` client + pure nudge helper + tests

**Files:** `web/lib/activation.ts`, `web/lib/activation.test.ts`.

**Interfaces:**
- Produces: `getActivation(token) → Promise<Activation>`; types `ActivationStep`, `Activation`; pure
  `shouldNudge(used, limit, threshold?) → boolean`.

- [ ] **Step 1: `web/lib/activation.ts`:**
```ts
import { CORE_URL } from "./api";

export type ActivationStep = {
  key: string;
  label: string;
  done: boolean;
  cta_href: string;
  hint: string;
};
export type Activation = {
  role: "seller" | "buyer";
  percent_complete: number;
  steps: ActivationStep[];
};

export async function getActivation(token: string): Promise<Activation> {
  const res = await fetch(`${CORE_URL}/v1/activation`, {
    headers: { authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return JSON.parse(text) as Activation;
}

// shouldNudge decides whether to show a near-limit upgrade nudge. Unlimited
// (limit < 0) never nudges; otherwise nudge once usage reaches `threshold` of
// the limit (default 80%). limit === 0 always nudges (nothing allowed on-plan).
export function shouldNudge(used: number, limit: number, threshold = 0.8): boolean {
  if (limit < 0) return false;
  if (limit === 0) return true;
  return used / limit >= threshold;
}
```

- [ ] **Step 2: `web/lib/activation.test.ts`** (mirror `lib/dashboard.test.ts` — mock `global.fetch`):
```ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { getActivation, shouldNudge } from "./activation";

const realFetch = global.fetch;
beforeEach(() => vi.restoreAllMocks());
afterEach(() => { global.fetch = realFetch; });
function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("activation client", () => {
  it("getActivation sends the token and parses the status", async () => {
    mockFetch(200, JSON.stringify({ role: "seller", percent_complete: 40, steps: [{ key: "profile", label: "P", done: true, cta_href: "/seller/profile", hint: "h" }] }));
    const a = await getActivation("tok");
    expect(a.role).toBe("seller");
    expect(a.percent_complete).toBe(40);
    expect(a.steps[0].done).toBe(true);
    const init = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][1] as RequestInit;
    expect((init.headers as Record<string, string>).authorization).toBe("Bearer tok");
  });
  it("getActivation throws on a non-OK response", async () => {
    mockFetch(401, "nope");
    await expect(getActivation("tok")).rejects.toThrow();
  });
});

describe("shouldNudge", () => {
  it("never nudges an unlimited plan", () => { expect(shouldNudge(9999, -1)).toBe(false); });
  it("always nudges when nothing is allowed (limit 0)", () => { expect(shouldNudge(0, 0)).toBe(true); });
  it("nudges at/over the 80% threshold", () => {
    expect(shouldNudge(8, 10)).toBe(true);
    expect(shouldNudge(7, 10)).toBe(false);
    expect(shouldNudge(10, 10)).toBe(true);
  });
});
```

- [ ] **Step 3: Run** `npm test` → green; `npx tsc --noEmit` → clean.
- [ ] **Step 4: Commit** — `feat(web): activation lib client + shouldNudge helper + tests`.

---

### Task 2: `<EmptyState>`, `<ActivationChecklist>`, `<UpgradeNudge>` components

**Files:** `web/components/EmptyState.tsx`, `web/components/ActivationChecklist.tsx`,
`web/components/UpgradeNudge.tsx`.

**Interfaces:**
- Produces: `<EmptyState title body? cta?>`, `<ActivationChecklist>` (self-fetching),
  `<UpgradeNudge feature used limit planRequired?>`. Consumed by Plans 3 & 4.

- [ ] **Step 1: `web/components/EmptyState.tsx`** — presentational, no dead-ends:
```tsx
import Link from "next/link";

export function EmptyState({
  title,
  body,
  cta,
}: {
  title: string;
  body?: string;
  cta?: { label: string; href: string };
}) {
  return (
    <div className="card" style={{ textAlign: "center", padding: "2rem 1rem" }}>
      <h3 style={{ margin: "0 0 0.4rem" }}>{title}</h3>
      {body && <p className="muted" style={{ margin: "0 0 1rem", maxWidth: 420, marginInline: "auto" }}>{body}</p>}
      {cta && (
        <Link href={cta.href} className="btn" style={{ display: "inline-block" }}>
          {cta.label}
        </Link>
      )}
    </div>
  );
}
```

- [ ] **Step 2: `web/components/ActivationChecklist.tsx`** — self-fetching; hides at 100%:
```tsx
"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useToken } from "@/lib/auth";
import { getActivation, type Activation } from "@/lib/activation";

export function ActivationChecklist() {
  const token = useToken();
  const [act, setAct] = useState<Activation | null>(null);

  useEffect(() => {
    if (!token) return;
    getActivation(token).then(setAct).catch(() => {});
  }, [token]);

  if (!act || act.percent_complete >= 100) return null;

  return (
    <div className="card" style={{ marginBottom: "1.5rem" }}>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0, fontSize: "1.1rem" }}>Get started</h2>
        <span className="muted small">{act.percent_complete}% complete</span>
      </div>
      <div role="progressbar" aria-valuenow={act.percent_complete} aria-valuemin={0} aria-valuemax={100}
        style={{ height: 6, borderRadius: 3, background: "var(--border, #e5e7eb)", margin: "0.6rem 0 1rem", overflow: "hidden" }}>
        <div style={{ width: `${act.percent_complete}%`, height: "100%", background: "var(--accent, #2563eb)" }} />
      </div>
      <ul className="col" style={{ listStyle: "none", padding: 0, margin: 0, gap: "0.5rem" }}>
        {act.steps.map((s) => (
          <li key={s.key} className="row" style={{ justifyContent: "space-between", alignItems: "center", gap: "0.75rem" }}>
            <span className="row" style={{ gap: "0.5rem", alignItems: "center" }}>
              <span aria-hidden style={{ color: s.done ? "var(--ok, #16a34a)" : "var(--muted, #9ca3af)" }}>{s.done ? "✓" : "○"}</span>
              <span style={{ textDecoration: s.done ? "line-through" : "none" }}>
                {s.label}
                <span className="muted small" style={{ display: "block" }}>{s.hint}</span>
              </span>
            </span>
            {!s.done && <Link href={s.cta_href} className="btn small">Do it</Link>}
          </li>
        ))}
      </ul>
    </div>
  );
}
```
> If `.btn.small` compound isn't a real class, use `className="btn"` with an inline `fontSize:"0.85rem"`.
> The CSS vars (`--border/--accent/--ok/--muted`) have inline fallbacks so they render regardless.

- [ ] **Step 3: `web/components/UpgradeNudge.tsx`** — near-limit banner + locked CTA, reusing `UpgradeModal`:
```tsx
"use client";

import { useState } from "react";
import UpgradeModal from "@/components/UpgradeModal";
import { usageLabel } from "@/lib/billing";
import { shouldNudge } from "@/lib/activation";

// UpgradeNudge shows a near-limit banner when usage approaches the plan limit,
// or a locked-feature CTA when `locked`. Both open the shared UpgradeModal.
export function UpgradeNudge({
  feature,
  used,
  limit,
  planRequired = "pro",
  locked = false,
}: {
  feature: string;
  used?: number;
  limit?: number;
  planRequired?: string;
  locked?: boolean;
}) {
  const [open, setOpen] = useState(false);

  const near = !locked && typeof used === "number" && typeof limit === "number" && shouldNudge(used, limit);
  if (!locked && !near) return null;

  return (
    <>
      <div className="card" style={{ borderLeft: "3px solid var(--accent, #2563eb)", marginBottom: "1rem" }}>
        <div className="row" style={{ justifyContent: "space-between", alignItems: "center", gap: "0.75rem", flexWrap: "wrap" }}>
          <span className="small">
            {locked
              ? "This feature is on a higher plan."
              : `You're near your plan limit${typeof used === "number" && typeof limit === "number" ? ` (${usageLabel(used, limit)})` : ""}.`}
          </span>
          <button className="btn" onClick={() => setOpen(true)}>Upgrade</button>
        </div>
      </div>
      {open && (
        <UpgradeModal
          feature={feature}
          planRequired={planRequired}
          onClose={() => setOpen(false)}
          onUpgrade={() => { window.location.href = "/settings/billing"; }}
        />
      )}
    </>
  );
}
```

- [ ] **Step 4: Verify** `npx tsc --noEmit` clean; `npm run build` exits 0 (do NOT pipe). `npm test` still
  green (no new lib tests here, but confirm nothing broke).
- [ ] **Step 5: Commit** — `feat(web): EmptyState + ActivationChecklist + UpgradeNudge components`.

---

## Self-Review checklist

- `getActivation` sends the bearer token, parses `Activation`, throws on non-OK; `shouldNudge` covers
  unlimited/zero/threshold — all unit-tested (`npm test` green).
- `<EmptyState>` always offers a next action (no dead-end); `<ActivationChecklist>` auto-hides at 100% and
  has an accessible progressbar; `<UpgradeNudge>` reuses `UpgradeModal`/`usageLabel`/`shouldNudge` and
  renders nothing when not near-limit and not locked.
- `tsc --noEmit` clean; `npm run build` exits 0 (never piped); nothing pushed.
