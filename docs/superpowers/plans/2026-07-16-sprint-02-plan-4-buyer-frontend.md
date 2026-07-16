# Sprint 2 — Plan 4: Buyer Frontend (billing UI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the freemium engine visible and usable in the buyer app (`core/web`): a pricing page, a contextual upgrade paywall that fires on any `402`, a billing/usage screen with meters, a team/seats screen whose invite respects the seat cap, and the Stripe Checkout redirect flow.

**Architecture:** All billing network logic + the `402 {code,feature,plan_required}` → typed `PaywallError` parsing lives in a tested `lib/billing.ts` (mirrors Sprint 1's `lib/notifications.ts` `call<T>` pattern). React components/pages are thin consumers built in the existing dark-theme, CSS-variable, utility-class idiom (`AppShell`, `PageHeader`, `.card/.btn/.field/.tbl`). Per the Sprint 1 precedent, **only lib logic is unit-tested (vitest); components/pages are verified by `tsc --noEmit` + `next build`** (no React Testing Library).

**Tech Stack:** Next.js 16 (App Router) / React 19 / TypeScript 5 strict; buyer app talks to Core `:8080` with the `tb_core_token`. Plain global CSS tokens (no Tailwind/CSS Modules).

## Global Constraints

- **Paywall contract:** gated endpoints return HTTP **402** with `{"code":"upgrade_required","feature":<string>,"plan_required":<string>}`. `lib/billing.ts` parses that into a `PaywallError { feature, planRequired }`; the UI catches it and shows `<UpgradeModal>`.
- **Entitlement/summary shape** (from `GET /v1/billing/summary`, Plan 1): `{plan, entitlements:{agent_autonomy,catalog_max_items,rfq_monthly_limit,seats}, usage:{rfq_used,rfq_limit,catalog_used,catalog_limit,seats_used,seats_limit}}`. A limit of `-1` means unlimited.
- **Checkout:** `POST /v1/billing/checkout {plan,interval}` → `{url}` (redirect the browser to it). In stub/dev the URL is `https://stub.local/...` — that's expected; the flow is real, the destination isn't until Stripe keys are set.
- **Testing split:** `lib/billing.ts` → vitest (`lib/billing.test.ts`), mocking `global.fetch` exactly like `lib/notifications.test.ts`. Components/pages → no unit test; the task's gate is `npx tsc --noEmit` + `npm run build` clean.
- **Styling:** reuse existing tokens (`var(--surface)`, `var(--primary)`, `var(--muted)`, `var(--border)`, …) and utility classes (`.card .btn .btn-primary .btn-sm .btn-ghost .field .input .select .grid .row .col .tbl .badge .badge-info .badge-success .notice .muted .faint .stat`). No new CSS framework. A small amount of scoped CSS in `globals.css` is acceptable for the modal/meter if a utility doesn't exist.
- **Buyer app only** (seller-app billing UI is deferred per the sprint decision). Nav links are added to `AppShell`.
- **Local only:** feature branch, never push. `web/` is the working dir for all commands.

## File Structure

- `web/lib/billing.ts` — types, `PaywallError`, `call<T>` (402-aware), `getBillingSummary`/`startCheckout`/`openBillingPortal`/`listMembers`/`inviteMember`, `usagePercent`/`usageLabel` helpers.
- `web/lib/billing.test.ts` — vitest for all of the above.
- `web/components/UpgradeModal.tsx`, `web/components/UsageMeter.tsx`.
- `web/app/pricing/page.tsx` — public pricing/comparison page.
- `web/app/(app)/settings/billing/page.tsx` — plan + portal + usage meters + checkout-return handling.
- `web/app/(app)/settings/team/page.tsx` — members + invite (seat-cap paywall).
- `web/components/AppShell.tsx` — add Billing + Team nav items.

---

## Task 1: `lib/billing.ts` + tests

**Files:**
- Create: `web/lib/billing.ts`, `web/lib/billing.test.ts`

**Interfaces:**
- Produces: `Plan`, `Entitlements`, `Usage`, `BillingSummary`, `Member` types; `class PaywallError`; `getBillingSummary(token)`, `startCheckout(token, plan, interval)`, `openBillingPortal(token)`, `listMembers(token)`, `inviteMember(token, email, role)`; `usagePercent(used, limit)`, `usageLabel(used, limit)`.

- [ ] **Step 1: Write the failing test**

Create `web/lib/billing.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  getBillingSummary, startCheckout, inviteMember, listMembers,
  usagePercent, usageLabel, PaywallError,
} from "./billing";

const realFetch = global.fetch;
beforeEach(() => { vi.restoreAllMocks(); });
afterEach(() => { global.fetch = realFetch; });

function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("billing client", () => {
  it("parses the billing summary", async () => {
    mockFetch(200, JSON.stringify({
      plan: "free",
      entitlements: { agent_autonomy: false, catalog_max_items: 25, rfq_monthly_limit: 10, seats: 2 },
      usage: { rfq_used: 3, rfq_limit: 10, catalog_used: 1, catalog_limit: 25, seats_used: 1, seats_limit: 2 },
    }));
    const s = await getBillingSummary("tok");
    expect(s.plan).toBe("free");
    expect(s.entitlements.agent_autonomy).toBe(false);
    expect(s.usage.rfq_used).toBe(3);
  });

  it("startCheckout returns the redirect url", async () => {
    mockFetch(200, JSON.stringify({ url: "https://stub.local/checkout?x=1" }));
    const url = await startCheckout("tok", "pro", "month");
    expect(url).toContain("checkout");
    const init = (global.fetch as any).mock.calls[0][1];
    expect(JSON.parse(init.body)).toEqual({ plan: "pro", interval: "month" });
  });

  it("a 402 becomes a PaywallError carrying feature + plan", async () => {
    mockFetch(402, JSON.stringify({ code: "upgrade_required", feature: "seats", plan_required: "pro" }));
    await expect(inviteMember("tok", "a@b.com", "member")).rejects.toBeInstanceOf(PaywallError);
    try {
      mockFetch(402, JSON.stringify({ code: "upgrade_required", feature: "seats", plan_required: "pro" }));
      await inviteMember("tok", "a@b.com", "member");
    } catch (e) {
      expect(e).toBeInstanceOf(PaywallError);
      expect((e as PaywallError).feature).toBe("seats");
      expect((e as PaywallError).planRequired).toBe("pro");
    }
  });

  it("listMembers parses the users array", async () => {
    mockFetch(200, JSON.stringify({ users: [{ id: "u1", email: "a@b.com", role: "owner" }] }));
    const m = await listMembers("tok");
    expect(m[0].email).toBe("a@b.com");
  });

  it("usagePercent clamps and treats -1 as unlimited (0%)", () => {
    expect(usagePercent(5, 10)).toBe(50);
    expect(usagePercent(20, 10)).toBe(100); // clamp
    expect(usagePercent(3, -1)).toBe(0);    // unlimited
    expect(usagePercent(0, 0)).toBe(100);   // zero cap = full
  });

  it("usageLabel renders unlimited as ∞", () => {
    expect(usageLabel(3, 10)).toBe("3 / 10");
    expect(usageLabel(3, -1)).toBe("3 / ∞");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run (from `web/`): `npm run test -- billing`
Expected: FAIL — `./billing` module not found.

- [ ] **Step 3: Write the client**

Create `web/lib/billing.ts`:

```ts
import { CORE_URL } from "./api";

export type Plan = "free" | "pro" | "business";
export type Interval = "month" | "year";

export type Entitlements = {
  agent_autonomy: boolean;
  catalog_max_items: number;
  rfq_monthly_limit: number;
  seats: number;
};
export type Usage = {
  rfq_used: number;
  rfq_limit: number;
  catalog_used: number;
  catalog_limit: number;
  seats_used: number;
  seats_limit: number;
};
export type BillingSummary = { plan: Plan; entitlements: Entitlements; usage: Usage };
export type Member = { id: string; email: string; role: string };

/** PaywallError is thrown when a gated endpoint returns 402 upgrade_required. */
export class PaywallError extends Error {
  readonly code = "upgrade_required";
  feature: string;
  planRequired: string;
  constructor(feature: string, planRequired: string) {
    super(`Upgrade required${feature ? ` for ${feature}` : ""}`);
    this.name = "PaywallError";
    this.feature = feature;
    this.planRequired = planRequired;
  }
}

// Local request helper mirroring lib/notifications.ts, but 402-aware: a
// 402 upgrade_required body becomes a PaywallError so callers can show the
// paywall instead of a raw error string.
async function call<T>(
  method: string,
  path: string,
  opts: { token?: string | null; body?: unknown } = {},
): Promise<T> {
  const res = await fetch(`${CORE_URL}${path}`, {
    method,
    headers: {
      ...(opts.body ? { "content-type": "application/json" } : {}),
      ...(opts.token ? { authorization: `Bearer ${opts.token}` } : {}),
    },
    body: opts.body ? JSON.stringify(opts.body) : undefined,
    cache: "no-store",
  });
  const text = await res.text();
  if (res.status === 402) {
    let feature = "";
    let planRequired = "pro";
    try {
      const j = JSON.parse(text) as { feature?: string; plan_required?: string };
      feature = j.feature ?? "";
      planRequired = j.plan_required ?? "pro";
    } catch {
      /* keep defaults */
    }
    throw new PaywallError(feature, planRequired);
  }
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return (text ? JSON.parse(text) : undefined) as T;
}

export function getBillingSummary(token: string): Promise<BillingSummary> {
  return call<BillingSummary>("GET", "/v1/billing/summary", { token });
}

export async function startCheckout(token: string, plan: Plan, interval: Interval): Promise<string> {
  const { url } = await call<{ url: string }>("POST", "/v1/billing/checkout", {
    token,
    body: { plan, interval },
  });
  return url;
}

export async function openBillingPortal(token: string): Promise<string> {
  const { url } = await call<{ url: string }>("POST", "/v1/billing/portal", { token });
  return url;
}

export async function listMembers(token: string): Promise<Member[]> {
  const { users } = await call<{ users: Member[] }>("GET", "/v1/users", { token });
  return users;
}

export function inviteMember(token: string, email: string, role: "admin" | "member"): Promise<{ token: string }> {
  return call<{ token: string }>("POST", "/v1/users/invite", { token, body: { email, role } });
}

/** usagePercent returns a 0–100 bar fill. -1 (unlimited) shows empty; a 0 cap shows full. */
export function usagePercent(used: number, limit: number): number {
  if (limit < 0) return 0;
  if (limit === 0) return 100;
  return Math.min(100, Math.round((used / limit) * 100));
}

/** usageLabel renders "used / limit", with ∞ for unlimited. */
export function usageLabel(used: number, limit: number): string {
  return `${used} / ${limit < 0 ? "∞" : limit}`;
}
```

- [ ] **Step 4: Run the tests**

Run: `npm run test -- billing`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add web/lib/billing.ts web/lib/billing.test.ts
git commit -m "feat(web): billing client (summary/checkout/portal/team) + 402 PaywallError"
```

---

## Task 2: `<UpgradeModal>` + `<UsageMeter>`

**Files:**
- Create: `web/components/UpgradeModal.tsx`, `web/components/UsageMeter.tsx`

**Interfaces:**
- Consumes: `PaywallError`-ish props; `usagePercent`/`usageLabel` (Task 1).
- Produces: `<UpgradeModal feature planRequired onClose onUpgrade? busy? />`; `<UsageMeter label used limit />`.

- [ ] **Step 1: Write `<UsageMeter>`**

Create `web/components/UsageMeter.tsx`:

```tsx
"use client";

import { usageLabel, usagePercent } from "@/lib/billing";

/** A labelled usage bar. Unlimited (-1) renders a full-width "∞" with no fill. */
export default function UsageMeter({ label, used, limit }: { label: string; used: number; limit: number }) {
  const pct = usagePercent(used, limit);
  const near = limit >= 0 && pct >= 80;
  const unlimited = limit < 0;
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "0.35rem" }}>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <span style={{ fontWeight: 550 }}>{label}</span>
        <span className={near ? "" : "muted"} style={near ? { color: "var(--warning)" } : undefined}>
          {usageLabel(used, limit)}
        </span>
      </div>
      <div style={{ height: 8, borderRadius: 6, background: "var(--surface-2)", overflow: "hidden" }}>
        {!unlimited && (
          <div
            style={{
              height: "100%",
              width: `${pct}%`,
              background: near ? "var(--warning)" : "var(--primary)",
              transition: "width 200ms ease",
            }}
          />
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Write `<UpgradeModal>`**

Create `web/components/UpgradeModal.tsx`:

```tsx
"use client";

// Copy for each gated feature — names the specific benefit the plan unlocks.
const FEATURE_COPY: Record<string, string> = {
  agent_autonomy: "Let your agent send quotes 24/7 without manual review.",
  rfq_monthly_limit: "Post more RFQs each month.",
  catalog_max_items: "List more products in your catalog.",
  seats: "Invite more teammates.",
};

export default function UpgradeModal({
  feature,
  planRequired,
  onClose,
  onUpgrade,
  busy,
}: {
  feature: string;
  planRequired: string;
  onClose: () => void;
  onUpgrade?: () => void;
  busy?: boolean;
}) {
  const plan = planRequired ? planRequired[0].toUpperCase() + planRequired.slice(1) : "Pro";
  const benefit = FEATURE_COPY[feature] ?? "Unlock more of TradeBridge.";
  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      style={{
        position: "fixed", inset: 0, background: "rgba(0,0,0,0.6)",
        display: "flex", alignItems: "center", justifyContent: "center", padding: "1rem", zIndex: 50,
      }}
    >
      <div className="card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: 420, width: "100%" }}>
        <span className="badge badge-info" style={{ marginBottom: "0.75rem" }}>{plan} plan</span>
        <h2 style={{ fontSize: "1.2rem", marginBottom: "0.5rem" }}>Upgrade to unlock this</h2>
        <p className="muted" style={{ marginBottom: "1.25rem" }}>{benefit}</p>
        <div className="row" style={{ gap: "0.6rem", justifyContent: "flex-end" }}>
          <button className="btn btn-ghost" onClick={onClose} disabled={busy}>Not now</button>
          {onUpgrade && (
            <button className="btn btn-primary" onClick={onUpgrade} disabled={busy}>
              {busy ? "Redirecting…" : `Upgrade to ${plan}`}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Verify compile**

Run (from `web/`): `npx tsc --noEmit`
Expected: clean (no type errors).

- [ ] **Step 4: Commit**

```bash
git add web/components/UpgradeModal.tsx web/components/UsageMeter.tsx
git commit -m "feat(web): UpgradeModal + UsageMeter components"
```

---

## Task 3: `/pricing` page

**Files:**
- Create: `web/app/pricing/page.tsx`

**Interfaces:** Consumes `startCheckout` (Task 1), `useToken` (`lib/auth`). Public route (no AppShell guard).

Reviewer note: display prices + per-plan feature values here are **marketing copy** (the seeded defaults). Limits are admin-editable server-side, so a future follow-up may fetch a public plans endpoint; for now the table is static and clearly a marketing surface. Not a defect.

- [ ] **Step 1: Write the page**

Create `web/app/pricing/page.tsx`:

```tsx
"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useToken } from "@/lib/auth";
import { startCheckout, type Interval, type Plan } from "@/lib/billing";

type Tier = {
  plan: Plan;
  name: string;
  monthly: number; // display price (USD)
  blurb: string;
  features: string[];
  cta: string;
  highlight?: boolean;
};

const TIERS: Tier[] = [
  {
    plan: "free", name: "Free", monthly: 0, blurb: "Get on the marketplace.",
    features: ["25 catalog items", "10 RFQs / month", "2 seats", "Manual quote review"],
    cta: "Current plan",
  },
  {
    plan: "pro", name: "Pro", monthly: 49, blurb: "Let your agent quote 24/7.", highlight: true,
    features: ["500 catalog items", "100 RFQs / month", "5 seats", "Agent autonomy (auto-quoting)"],
    cta: "Upgrade to Pro",
  },
  {
    plan: "business", name: "Business", monthly: 199, blurb: "Scale with your team.",
    features: ["Unlimited catalog", "Unlimited RFQs", "Unlimited seats", "Agent autonomy + everything in Pro"],
    cta: "Upgrade to Business",
  },
];

export default function PricingPage() {
  const token = useToken();
  const router = useRouter();
  const [interval, setInterval] = useState<Interval>("month");
  const [busy, setBusy] = useState<Plan | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const choose = async (plan: Plan) => {
    if (plan === "free") return;
    if (!token) {
      router.push("/register");
      return;
    }
    setBusy(plan);
    setErr(null);
    try {
      const url = await startCheckout(token, plan, interval);
      window.location.href = url;
    } catch (e) {
      setErr(e instanceof Error ? e.message : "checkout failed");
      setBusy(null);
    }
  };

  const price = (m: number) => (interval === "year" ? Math.round(m * 10) : m); // 2 months free

  return (
    <main style={{ maxWidth: 980, margin: "0 auto", padding: "3rem 1.5rem" }}>
      <div style={{ textAlign: "center", marginBottom: "2rem" }}>
        <h1 style={{ fontSize: "2rem" }}>Pricing</h1>
        <p className="muted">Start free. Upgrade when your agent should quote for you.</p>
        <div className="row" style={{ justifyContent: "center", gap: "0.5rem", marginTop: "1rem" }}>
          <button className={`btn btn-sm ${interval === "month" ? "btn-primary" : "btn-ghost"}`} onClick={() => setInterval("month")}>Monthly</button>
          <button className={`btn btn-sm ${interval === "year" ? "btn-primary" : "btn-ghost"}`} onClick={() => setInterval("year")}>Annual · 2 months free</button>
        </div>
      </div>

      {err && <div className="notice err" style={{ marginBottom: "1rem" }}>{err}</div>}

      <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", gap: "1rem" }}>
        {TIERS.map((t) => (
          <div key={t.plan} className="card" style={t.highlight ? { borderColor: "var(--primary)" } : undefined}>
            {t.highlight && <span className="badge badge-info" style={{ marginBottom: "0.6rem" }}>Most popular</span>}
            <h2 style={{ fontSize: "1.25rem" }}>{t.name}</h2>
            <p className="muted" style={{ margin: "0.2rem 0 0.75rem" }}>{t.blurb}</p>
            <div style={{ fontSize: "1.8rem", fontWeight: 700 }}>
              ${price(t.monthly)}<span className="faint" style={{ fontSize: "0.9rem", fontWeight: 400 }}>/{interval === "year" ? "yr" : "mo"}</span>
            </div>
            <ul style={{ listStyle: "none", padding: 0, margin: "1rem 0", display: "flex", flexDirection: "column", gap: "0.4rem" }}>
              {t.features.map((f) => <li key={f} className="row" style={{ gap: "0.5rem" }}><span style={{ color: "var(--success)" }}>✓</span> {f}</li>)}
            </ul>
            <button
              className={`btn ${t.highlight ? "btn-primary" : ""}`}
              style={{ width: "100%" }}
              disabled={t.plan === "free" || busy !== null}
              onClick={() => choose(t.plan)}
            >
              {busy === t.plan ? "Redirecting…" : t.cta}
            </button>
          </div>
        ))}
      </div>

      <p className="muted small" style={{ textAlign: "center", marginTop: "1.5rem" }}>
        Already have an account? <Link href="/settings/billing">Manage billing</Link>
      </p>
    </main>
  );
}
```

- [ ] **Step 2: Verify compile + build**

Run (from `web/`): `npx tsc --noEmit && npm run build`
Expected: clean; `/pricing` appears in the route list.

- [ ] **Step 3: Commit**

```bash
git add web/app/pricing/page.tsx
git commit -m "feat(web): public /pricing page with monthly/annual toggle + checkout"
```

---

## Task 4: `/settings/billing` page + nav

**Files:**
- Create: `web/app/(app)/settings/billing/page.tsx`
- Modify: `web/components/AppShell.tsx` (add nav)

**Interfaces:** Consumes `getBillingSummary`/`openBillingPortal`/`startCheckout` (Task 1), `<UsageMeter>` (Task 2), `useToken`, `PageHeader`, `useSearchParams`.

- [ ] **Step 1: Write the page**

Create `web/app/(app)/settings/billing/page.tsx`:

```tsx
"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { PageHeader } from "@/components/AppShell";
import UsageMeter from "@/components/UsageMeter";
import { useToken } from "@/lib/auth";
import { getBillingSummary, openBillingPortal, startCheckout, type BillingSummary } from "@/lib/billing";

function BillingInner() {
  const token = useToken();
  const params = useSearchParams();
  const justChecked = params.get("checkout") === "success";
  const [summary, setSummary] = useState<BillingSummary | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!token) return;
    let active = true;
    let tries = 0;
    const load = () => {
      getBillingSummary(token)
        .then((s) => {
          if (!active) return;
          setSummary(s);
          // After returning from Checkout, the webhook may lag; poll a few times.
          if (justChecked && s.plan === "free" && tries < 5) {
            tries += 1;
            setTimeout(load, 2000);
          }
        })
        .catch((e) => active && setErr(e instanceof Error ? e.message : "failed"));
    };
    load();
    return () => { active = false; };
  }, [token, justChecked]);

  const portal = async () => {
    if (!token) return;
    setBusy(true);
    try {
      window.location.href = await openBillingPortal(token);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "portal unavailable");
      setBusy(false);
    }
  };

  const upgrade = async (plan: "pro" | "business") => {
    if (!token) return;
    setBusy(true);
    try {
      window.location.href = await startCheckout(token, plan, "month");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "checkout failed");
      setBusy(false);
    }
  };

  return (
    <>
      <PageHeader title="Billing & usage" subtitle="Your plan, usage, and payment settings." />
      {justChecked && summary?.plan === "free" && (
        <div className="notice" style={{ marginBottom: "1rem" }}>Activating your subscription… this can take a few seconds.</div>
      )}
      {err && <div className="notice err" style={{ marginBottom: "1rem" }}>{err}</div>}
      {!summary ? (
        <p className="muted">Loading…</p>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: "1.25rem" }}>
          <div className="card">
            <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
              <div>
                <div className="faint small" style={{ textTransform: "uppercase", letterSpacing: "0.06em" }}>Current plan</div>
                <div style={{ fontSize: "1.5rem", fontWeight: 700, textTransform: "capitalize" }}>{summary.plan}</div>
              </div>
              <div className="row" style={{ gap: "0.5rem" }}>
                {summary.plan === "free"
                  ? <button className="btn btn-primary" disabled={busy} onClick={() => upgrade("pro")}>{busy ? "…" : "Upgrade to Pro"}</button>
                  : <button className="btn btn-ghost" disabled={busy} onClick={portal}>{busy ? "…" : "Manage in portal"}</button>}
              </div>
            </div>
          </div>

          <div className="card">
            <h2 style={{ fontSize: "1.05rem", marginBottom: "1rem" }}>Usage this month</h2>
            <div style={{ display: "flex", flexDirection: "column", gap: "1rem" }}>
              <UsageMeter label="RFQs posted" used={summary.usage.rfq_used} limit={summary.usage.rfq_limit} />
              <UsageMeter label="Catalog items" used={summary.usage.catalog_used} limit={summary.usage.catalog_limit} />
              <UsageMeter label="Team seats" used={summary.usage.seats_used} limit={summary.usage.seats_limit} />
            </div>
          </div>
        </div>
      )}
    </>
  );
}

export default function BillingPage() {
  return (
    <Suspense fallback={<p className="muted">Loading…</p>}>
      <BillingInner />
    </Suspense>
  );
}
```

- [ ] **Step 2: Add nav items to AppShell**

In `web/components/AppShell.tsx`, add a new nav section (after the `Seller` group) that shows for everyone:

```tsx
  {
    group: "Settings",
    items: [
      { href: "/settings/billing", label: "Billing & usage", show: () => true },
      { href: "/settings/team", label: "Team", show: () => true },
    ],
  },
```

(Insert this object into the `NAV` array. The `NavItem`/`show` types already exist.)

- [ ] **Step 3: Verify compile + build**

Run: `npx tsc --noEmit && npm run build`
Expected: clean; `/settings/billing` in the route list. (`useSearchParams` MUST be inside a `<Suspense>` boundary — it is — or the build fails.)

- [ ] **Step 4: Commit**

```bash
git add "web/app/(app)/settings/billing/page.tsx" web/components/AppShell.tsx
git commit -m "feat(web): /settings/billing (plan, usage meters, portal, checkout-return poll)"
```

---

## Task 5: `/settings/team` page + seat-cap paywall

**Files:**
- Create: `web/app/(app)/settings/team/page.tsx`

**Interfaces:** Consumes `listMembers`/`inviteMember`/`getBillingSummary` (Task 1), `PaywallError` (Task 1), `<UpgradeModal>` (Task 2), `useToken`, `PageHeader`.

- [ ] **Step 1: Write the page**

Create `web/app/(app)/settings/team/page.tsx`:

```tsx
"use client";

import { useEffect, useState } from "react";
import { PageHeader } from "@/components/AppShell";
import UpgradeModal from "@/components/UpgradeModal";
import UsageMeter from "@/components/UsageMeter";
import { useToken } from "@/lib/auth";
import {
  getBillingSummary, inviteMember, listMembers, startCheckout,
  PaywallError, type BillingSummary, type Member,
} from "@/lib/billing";

export default function TeamPage() {
  const token = useToken();
  const [members, setMembers] = useState<Member[]>([]);
  const [summary, setSummary] = useState<BillingSummary | null>(null);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<"admin" | "member">("member");
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [paywall, setPaywall] = useState<PaywallError | null>(null);

  const refresh = () => {
    if (!token) return;
    listMembers(token).then(setMembers).catch((e) => setErr(e.message));
    getBillingSummary(token).then(setSummary).catch(() => {});
  };

  useEffect(refresh, [token]);

  const invite = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true);
    setErr(null);
    setOk(null);
    try {
      await inviteMember(token, email, role);
      setOk(`Invited ${email}.`);
      setEmail("");
      refresh();
    } catch (e) {
      if (e instanceof PaywallError) setPaywall(e);
      else setErr(e instanceof Error ? e.message : "invite failed");
    } finally {
      setBusy(false);
    }
  };

  const upgrade = async () => {
    if (!token || !paywall) return;
    const plan = paywall.planRequired === "business" ? "business" : "pro";
    try {
      window.location.href = await startCheckout(token, plan, "month");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "checkout failed");
      setPaywall(null);
    }
  };

  return (
    <>
      <PageHeader title="Team" subtitle="Invite teammates. Seats are limited by your plan." />
      {summary && (
        <div className="card" style={{ marginBottom: "1.25rem" }}>
          <UsageMeter label="Seats used" used={summary.usage.seats_used} limit={summary.usage.seats_limit} />
        </div>
      )}

      <form onSubmit={invite} className="card" style={{ marginBottom: "1.25rem" }}>
        <h2 style={{ fontSize: "1.05rem", marginBottom: "0.75rem" }}>Invite a teammate</h2>
        <div className="row" style={{ gap: "0.6rem", flexWrap: "wrap" }}>
          <input className="input" style={{ flex: 1, minWidth: 200 }} type="email" required placeholder="name@company.com"
            value={email} onChange={(e) => setEmail(e.target.value)} />
          <select className="select" value={role} onChange={(e) => setRole(e.target.value as "admin" | "member")}>
            <option value="member">Member</option>
            <option value="admin">Admin</option>
          </select>
          <button className="btn btn-primary" disabled={busy || !email}>{busy ? "…" : "Send invite"}</button>
        </div>
        {err && <div className="notice err" style={{ marginTop: "0.75rem" }}>{err}</div>}
        {ok && <div className="notice" style={{ marginTop: "0.75rem" }}>{ok}</div>}
      </form>

      <div className="card">
        <h2 style={{ fontSize: "1.05rem", marginBottom: "0.75rem" }}>Members</h2>
        <table className="tbl" style={{ width: "100%" }}>
          <thead><tr><th style={{ textAlign: "left" }}>Email</th><th style={{ textAlign: "left" }}>Role</th></tr></thead>
          <tbody>
            {members.map((m) => (
              <tr key={m.id}><td>{m.email}</td><td><span className="badge">{m.role}</span></td></tr>
            ))}
          </tbody>
        </table>
      </div>

      {paywall && (
        <UpgradeModal
          feature={paywall.feature}
          planRequired={paywall.planRequired}
          onClose={() => setPaywall(null)}
          onUpgrade={upgrade}
        />
      )}
    </>
  );
}
```

- [ ] **Step 2: Verify compile + build + full web suite**

Run (from `web/`): `npx tsc --noEmit && npm run test && npm run build`
Expected: types clean; vitest (incl. Task 1's billing tests) green; build lists `/settings/team`.

- [ ] **Step 3: Commit**

```bash
git add "web/app/(app)/settings/team/page.tsx"
git commit -m "feat(web): /settings/team with seat-cap upgrade paywall"
```

---

## Self-Review (completed by plan author)

- **Spec coverage:** pricing page + comparison table ✓ (Task 3); contextual upgrade paywall firing on any 402, naming the benefit ✓ (Task 1 `PaywallError` + Task 2 `UpgradeModal`, wired at the seat gate in Task 5); billing/usage screen with meters + near-limit nudge ✓ (Task 4 + `UsageMeter` warning color at ≥80%); upgrade via Checkout, webhook-confirmed plan state ✓ (Task 3/4 `startCheckout` redirect + Task 4 summary-poll on return); members & invites UI + seat-limit paywall ✓ (Task 5). **Deferred (correctly):** seller-app billing UI (sprint decision); wiring the RFQ-cap paywall into the chat flow (the paywall infra is reusable — noted as a follow-up, not built, to keep this plan buyer-billing-scoped).
- **Placeholder scan:** all steps carry complete code. Pricing display prices/features are explicitly marketing copy (flagged for the reviewer, not a defect).
- **Type consistency:** `BillingSummary`/`Entitlements`/`Usage`/`Member`/`Plan`/`Interval`/`PaywallError` and `usagePercent`/`usageLabel` are used consistently across the lib, components, and pages. `useSearchParams` is Suspense-wrapped (Task 4) per Next's App Router requirement.
- **Testing honesty:** only `lib/billing.ts` is unit-tested (vitest), matching the Sprint 1 no-RTL precedent; every page/component task's gate is `tsc --noEmit` + `next build`. This is stated in Global Constraints so the coverage boundary is explicit, not silent.
