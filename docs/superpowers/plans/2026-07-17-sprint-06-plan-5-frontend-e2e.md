# Sprint 6 · Plan 5 — Frontend (Marketplace + Dashboards + Analytics) + E2E #6 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.
> Repos: `/home/deploy/b2b/tradebridge-core/web` (Next.js 16 / React 19 / TS strict) for Tasks 1–4,
> `/home/deploy/b2b/tradebridge-core` (Go) for Task 5. Commit per task.

**Goal:** Ship the Sprint-6 UI on top of the Plan 1–4 APIs — a public marketplace (search + facets +
ranked cards) and category pages, real buyer & seller dashboards, a seller analytics panel, and `/rfqs`
tabs/search with new-quote badges — then a spanning **E2E #6** proving discovery + both dashboards render
real aggregations.

**Architecture:** Three new `lib/*` clients (`discovery`, `dashboard`, `analytics`) following the existing
`CORE_URL` + local-`fetch` pattern (public discovery uses no token). New public route group
`app/(marketplace)/`. The `/dashboard` page splits by role into real tiles. `/rfqs` gains tabs + search.
E2E #6 is a Go integration test reusing the existing harness.

**Tech Stack:** Next.js App Router (client components), TypeScript strict, vitest (node env, mocked fetch);
Go/Fiber integration test.

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never push**.
- Web verification: `npm test` (vitest), `npx tsc --noEmit`, and `npm run build` — **never pipe `npm run
  build` through `head`/other** (SIGPIPE corrupts `.next`). Run from `web/`.
- E2E verification (Task 5): `export PATH="$HOME/.local/bin:$PATH"; export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#'); go test ./test/integration/ -run TestE2E06 -v` then full `go test ./... -p 1`.
- Reuse existing CSS utilities (`.card .stat .badge .tbl .btn .muted .ok .err .small .grid`) and components
  (`PageHeader` from `@/components/AppShell`, `VerifiedBadge`, `StarRating`). Do NOT invent a new design
  system or add CSS frameworks.
- Public marketplace pages must work logged-out (no token, no AppShell guard).

## Reference (verified conventions)

- `lib/api.ts`: `CORE_URL`, private `req<T>`; `listRfqs(token)` → `{rfqs: RFQ[], count}`; `RFQ` type has
  `id,title,raw_text,requirements,category,status,match_count,messages?,created_at`.
- Client pattern (public, no token) — see `lib/assist.ts` (local `fetch` on `CORE_URL`, `res.text()` then
  `JSON.parse`). Auth hooks: `useToken()`, `useMe()` (`{token, me, ready}`), `me.company_type` ∈
  buyer|seller|both|undefined (buyer = not 'seller').
- vitest config: `test.include=["lib/**/*.test.ts"]`, node env; tests mock `global.fetch` (see
  `lib/assist.test.ts`).
- Public storefront page `app/s/[id]/page.tsx` is the model for a public, tokenless client page.
- API shapes (from Plans 1–4):
  - `GET /public/v1/discovery?q=&category=&verified=&location=&min_rating=&sort=&page=&per_page=` →
    `{results:[{company_id,name,verification_status,verified_domain,description,categories,location,verified,avg_rating,review_count,score,matched_catalog?:[{name,category}]}], page, per_page, total}`.
  - `GET /public/v1/discovery/facets?...` → `{categories:[{value,count}], verified:{true,false}, rating_buckets:[{min,count}]}`.
  - `GET /v1/dashboard/buyer` → `{active_rfqs,quotes_received,new_quotes,orders_in_progress,recent_rfqs:[{id,title,status,quote_count,new_quotes,created_at}]}`.
  - `GET /v1/dashboard/seller` → `{incoming_rfqs,review_queue,active_orders,agent:{online,last_seen,autonomy},plan_usage:{plan,rfq_used,rfq_limit,catalog_used,catalog_limit,seats_used,seats_limit}}`.
  - `GET /v1/analytics/seller` → `{quotes_submitted,quotes_won,win_rate,revenue_won_cents,avg_response_hours,top_buyers:[{company_id,name,orders,revenue_cents}]}`.
  - `GET /v1/rfqs?tab=&q=` → `{rfqs:[RFQ + quote_count?,new_quotes?], count}` (badge fields omitted when 0 —
    treat absent as 0).

## File Structure

- Create: `web/lib/discovery.ts` + `web/lib/discovery.test.ts`
- Create: `web/lib/dashboard.ts` + `web/lib/dashboard.test.ts`
- Create: `web/lib/analytics.ts` + `web/lib/analytics.test.ts`
- Modify: `web/lib/api.ts` (`listRfqs` accepts `{tab?,q?}`; `RFQ` gains `quote_count?`,`new_quotes?`)
- Create: `web/app/(marketplace)/layout.tsx`, `web/app/(marketplace)/marketplace/page.tsx`,
  `web/app/(marketplace)/marketplace/[category]/page.tsx`
- Create: `web/components/DiscoveryResults.tsx` (shared search UI)
- Modify: `web/app/(app)/dashboard/page.tsx` (real role-split dashboards)
- Create: `web/components/SellerAnalytics.tsx`
- Modify: `web/app/(app)/rfqs/page.tsx` (tabs + search + badges)
- Create: `web/test/integration` → `test/integration/e2e_06_discovery_dashboards_test.go`

---

### Task 1: lib clients (discovery, dashboard, analytics) + tests

**Files:** `web/lib/discovery.ts`, `web/lib/dashboard.ts`, `web/lib/analytics.ts`, `web/lib/api.ts`, and the
three `.test.ts`.

**Interfaces:**
- Produces: `searchDiscovery(params)`, `getFacets(params)` (no token); `getBuyerDashboard(token)`,
  `getSellerDashboard(token)`; `getSellerAnalytics(token)`; extended `listRfqs(token, params?)`.

- [ ] **Step 1: `web/lib/discovery.ts`** (public, tokenless):
```ts
import { CORE_URL } from "./api";

export type DiscoveryResult = {
  company_id: string;
  name: string;
  verification_status: string;
  verified_domain: string | null;
  description: string | null;
  categories: string[];
  location: string | null;
  verified: boolean;
  avg_rating: number;
  review_count: number;
  score: number;
  matched_catalog?: { name: string; category: string }[];
};
export type DiscoveryPage = {
  results: DiscoveryResult[];
  page: number;
  per_page: number;
  total: number;
};
export type Facets = {
  categories: { value: string; count: number }[];
  verified: { true: number; false: number };
  rating_buckets: { min: number; count: number }[];
};
export type DiscoveryQuery = {
  q?: string;
  category?: string;
  location?: string;
  verified?: boolean;
  min_rating?: number;
  sort?: "relevance" | "rating" | "newest";
  page?: number;
  per_page?: number;
};

function qs(p: DiscoveryQuery): string {
  const s = new URLSearchParams();
  if (p.q) s.set("q", p.q);
  if (p.category) s.set("category", p.category);
  if (p.location) s.set("location", p.location);
  if (p.verified) s.set("verified", "true");
  if (p.min_rating) s.set("min_rating", String(p.min_rating));
  if (p.sort) s.set("sort", p.sort);
  if (p.page) s.set("page", String(p.page));
  if (p.per_page) s.set("per_page", String(p.per_page));
  return s.toString();
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${CORE_URL}${path}`, { cache: "no-store" });
  const text = await res.text();
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return (text ? JSON.parse(text) : undefined) as T;
}

export const searchDiscovery = (p: DiscoveryQuery) =>
  get<DiscoveryPage>(`/public/v1/discovery?${qs(p)}`);
export const getFacets = (p: DiscoveryQuery) =>
  get<Facets>(`/public/v1/discovery/facets?${qs(p)}`);
```

- [ ] **Step 2: `web/lib/dashboard.ts`** (authed):
```ts
import { CORE_URL } from "./api";

export type RecentRFQ = {
  id: string; title: string; status: string;
  quote_count: number; new_quotes: number; created_at: string;
};
export type BuyerDashboard = {
  active_rfqs: number; quotes_received: number; new_quotes: number;
  orders_in_progress: number; recent_rfqs: RecentRFQ[];
};
export type SellerDashboard = {
  incoming_rfqs: number; review_queue: number; active_orders: number;
  agent: { online: boolean; last_seen: string | null; autonomy: boolean };
  plan_usage: {
    plan: string; rfq_used: number; rfq_limit: number;
    catalog_used: number; catalog_limit: number; seats_used: number; seats_limit: number;
  };
};

async function authed<T>(token: string, path: string): Promise<T> {
  const res = await fetch(`${CORE_URL}${path}`, {
    headers: { authorization: `Bearer ${token}` }, cache: "no-store",
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return (text ? JSON.parse(text) : undefined) as T;
}

export const getBuyerDashboard = (token: string) => authed<BuyerDashboard>(token, "/v1/dashboard/buyer");
export const getSellerDashboard = (token: string) => authed<SellerDashboard>(token, "/v1/dashboard/seller");
```

- [ ] **Step 3: `web/lib/analytics.ts`** (authed):
```ts
import { CORE_URL } from "./api";

export type SellerAnalytics = {
  quotes_submitted: number; quotes_won: number; win_rate: number;
  revenue_won_cents: number; avg_response_hours: number;
  top_buyers: { company_id: string; name: string; orders: number; revenue_cents: number }[];
};

export const getSellerAnalytics = async (token: string): Promise<SellerAnalytics> => {
  const res = await fetch(`${CORE_URL}/v1/analytics/seller`, {
    headers: { authorization: `Bearer ${token}` }, cache: "no-store",
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return JSON.parse(text) as SellerAnalytics;
};
```

- [ ] **Step 4: Extend `web/lib/api.ts`.** Add optional badge fields to `RFQ`:
```ts
  quote_count?: number;
  new_quotes?: number;
```
  and make `listRfqs` accept filters (keep the no-arg call working):
```ts
export const listRfqs = (token: string, params?: { tab?: string; q?: string }) => {
  const s = new URLSearchParams();
  if (params?.tab) s.set("tab", params.tab);
  if (params?.q) s.set("q", params.q);
  const suffix = s.toString() ? `?${s.toString()}` : "";
  return req<{ rfqs: RFQ[]; count: number }>("GET", `/v1/rfqs${suffix}`, { token });
};
```

- [ ] **Step 5: Write the three test files** (`discovery.test.ts`, `dashboard.test.ts`, `analytics.test.ts`)
  mirroring `lib/assist.test.ts` (mock `global.fetch`). Each must assert: the correct URL/path + query
  string is built, the auth header is set where applicable (dashboard/analytics), the parsed shape returns,
  and a non-OK response throws. Example (`discovery.test.ts`):
```ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { searchDiscovery, getFacets } from "./discovery";

const realFetch = global.fetch;
beforeEach(() => vi.restoreAllMocks());
afterEach(() => { global.fetch = realFetch; });
function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("discovery client", () => {
  it("searchDiscovery builds the query and returns results", async () => {
    mockFetch(200, JSON.stringify({ results: [{ company_id: "c1", name: "Acme" }], page: 1, per_page: 20, total: 1 }));
    const r = await searchDiscovery({ q: "boxes", verified: true, category: "packaging" });
    expect(r.results[0].name).toBe("Acme");
    const url = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][0] as string;
    expect(url).toContain("/public/v1/discovery?");
    expect(url).toContain("q=boxes");
    expect(url).toContain("verified=true");
    expect(url).toContain("category=packaging");
  });
  it("getFacets returns facet buckets", async () => {
    mockFetch(200, JSON.stringify({ categories: [{ value: "packaging", count: 3 }], verified: { true: 2, false: 1 }, rating_buckets: [] }));
    const f = await getFacets({ q: "boxes" });
    expect(f.categories[0].count).toBe(3);
  });
  it("throws on a non-OK response", async () => {
    mockFetch(500, "boom");
    await expect(searchDiscovery({ q: "x" })).rejects.toThrow();
  });
});
```
  Write analogous `dashboard.test.ts` (assert `authorization: Bearer tok` header + parsed counts) and
  `analytics.test.ts` (assert win_rate/top_buyers parse).

- [ ] **Step 6: Run** `npm test` → all green. `npx tsc --noEmit` → clean.
- [ ] **Step 7: Commit** — `feat(web): discovery/dashboard/analytics lib clients + tests`.

---

### Task 2: Public marketplace + category pages

**Files:** `web/app/(marketplace)/layout.tsx`, `web/app/(marketplace)/marketplace/page.tsx`,
`web/app/(marketplace)/marketplace/[category]/page.tsx`, `web/components/DiscoveryResults.tsx`.

- [ ] **Step 1: `web/app/(marketplace)/layout.tsx`** — a minimal public shell (no AppShell/auth):
```tsx
export default function MarketplaceLayout({ children }: { children: React.ReactNode }) {
  return <main style={{ maxWidth: 1080, margin: "2rem auto", padding: "0 1rem" }}>{children}</main>;
}
```

- [ ] **Step 2: `web/components/DiscoveryResults.tsx`** — the shared search UI (client component). It owns the
  query state, fetches results + facets, and renders a facet sidebar + ranked cards. `initialCategory`
  presets the category (used by the category page).
```tsx
"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { VerifiedBadge } from "@/components/VerifiedBadge";
import { StarRating } from "@/components/StarRating";
import { searchDiscovery, getFacets, type DiscoveryResult, type Facets, type DiscoveryQuery } from "@/lib/discovery";

export function DiscoveryResults({ initialCategory }: { initialCategory?: string }) {
  const [q, setQ] = useState("");
  const [category, setCategory] = useState(initialCategory ?? "");
  const [verified, setVerified] = useState(false);
  const [minRating, setMinRating] = useState(0);
  const [sort, setSort] = useState<DiscoveryQuery["sort"]>("relevance");
  const [results, setResults] = useState<DiscoveryResult[]>([]);
  const [facets, setFacets] = useState<Facets | null>(null);
  const [total, setTotal] = useState(0);
  const [err, setErr] = useState<string | null>(null);

  const query: DiscoveryQuery = { q, category, verified, min_rating: minRating, sort };

  const run = useCallback(() => {
    searchDiscovery(query).then((p) => { setResults(p.results); setTotal(p.total); }).catch((e) => setErr(e.message));
    getFacets(query).then(setFacets).catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, category, verified, minRating, sort]);

  useEffect(() => { run(); }, [run]);

  return (
    <div style={{ display: "grid", gridTemplateColumns: "220px 1fr", gap: "1.5rem" }}>
      <aside className="col" style={{ gap: "1rem" }}>
        <div className="field">
          <label>Category</label>
          <div className="col" style={{ gap: "0.25rem" }}>
            <button className="btn" onClick={() => setCategory("")} aria-pressed={category === ""}>All</button>
            {facets?.categories.map((c) => (
              <button key={c.value} className="btn" aria-pressed={category === c.value}
                onClick={() => setCategory(c.value)}>{c.value} ({c.count})</button>
            ))}
          </div>
        </div>
        <label><input type="checkbox" checked={verified} onChange={(e) => setVerified(e.target.checked)} /> Verified only{facets ? ` (${facets.verified.true})` : ""}</label>
        <div className="field">
          <label>Min rating</label>
          <select className="input" value={minRating} onChange={(e) => setMinRating(Number(e.target.value))}>
            <option value={0}>Any</option><option value={4}>4+</option><option value={3}>3+</option><option value={2}>2+</option>
          </select>
        </div>
      </aside>

      <section className="col" style={{ gap: "1rem" }}>
        <div className="row" style={{ gap: "0.5rem" }}>
          <input className="input" placeholder="Search suppliers, products…" value={q} onChange={(e) => setQ(e.target.value)} style={{ flex: 1 }} />
          <select className="input" value={sort} onChange={(e) => setSort(e.target.value as DiscoveryQuery["sort"])}>
            <option value="relevance">Relevance</option><option value="rating">Rating</option><option value="newest">Newest</option>
          </select>
        </div>
        {err && <p className="err">{err}</p>}
        <p className="muted small">{total} supplier{total === 1 ? "" : "s"}</p>
        {results.map((r) => (
          <Link key={r.company_id} href={`/s/${r.company_id}`} className="card" style={{ display: "block" }}>
            <div className="row" style={{ gap: "0.5rem", alignItems: "center" }}>
              <strong>{r.name}</strong>
              <VerifiedBadge status={r.verification_status} />
              {r.review_count > 0 && <StarRating value={r.avg_rating} count={r.review_count} />}
            </div>
            {r.location && <p className="muted small" style={{ margin: "0.25rem 0" }}>{r.location}</p>}
            <p className="small" style={{ margin: "0.25rem 0" }}>{r.description}</p>
            <div className="row" style={{ gap: "0.35rem", flexWrap: "wrap" }}>
              {r.categories.map((c) => <span key={c} className="badge">{c}</span>)}
            </div>
            {r.matched_catalog && r.matched_catalog.length > 0 && (
              <p className="muted small" style={{ marginTop: "0.35rem" }}>
                Matched: {r.matched_catalog.map((m) => m.name).join(", ")}
              </p>
            )}
          </Link>
        ))}
        {results.length === 0 && !err && <p className="muted">No suppliers match yet.</p>}
      </section>
    </div>
  );
}
```
> If `VerifiedBadge`/`StarRating` prop names differ, open `web/components/VerifiedBadge.tsx` +
> `StarRating.tsx` and match (the storefront page uses `<VerifiedBadge status=... />` and
> `<StarRating value=... count=... />`). If `.col`/`.row`/`.field`/`.input` utility classes are missing from
> `globals.css`, use inline flex styles instead — do not add new global CSS.

- [ ] **Step 3: `web/app/(marketplace)/marketplace/page.tsx`:**
```tsx
import { DiscoveryResults } from "@/components/DiscoveryResults";

export default function MarketplacePage() {
  return (
    <>
      <h1>Find suppliers</h1>
      <p className="muted">Search verified suppliers by need, ranked by relevance and trust.</p>
      <DiscoveryResults />
    </>
  );
}
```

- [ ] **Step 4: `web/app/(marketplace)/marketplace/[category]/page.tsx`:**
```tsx
import { DiscoveryResults } from "@/components/DiscoveryResults";

export default async function CategoryPage({ params }: { params: Promise<{ category: string }> }) {
  const { category } = await params;
  const decoded = decodeURIComponent(category);
  return (
    <>
      <h1 style={{ textTransform: "capitalize" }}>{decoded}</h1>
      <p className="muted">Suppliers in {decoded}.</p>
      <DiscoveryResults initialCategory={decoded} />
    </>
  );
}
```
> Next.js 16: `params` is a Promise in server components — `await params`. `DiscoveryResults` is a client
> component, imported into a server component (allowed).

- [ ] **Step 5:** `npx tsc --noEmit` clean; `npm run build` exits 0 (do NOT pipe it).
- [ ] **Step 6: Commit** — `feat(web): public marketplace + category discovery pages`.

---

### Task 3: Real buyer & seller dashboards + analytics panel

**Files:** `web/app/(app)/dashboard/page.tsx`, `web/components/SellerAnalytics.tsx`.

- [ ] **Step 1: `web/components/SellerAnalytics.tsx`:**
```tsx
"use client";

import { useEffect, useState } from "react";
import { getSellerAnalytics, type SellerAnalytics } from "@/lib/analytics";

export function SellerAnalyticsPanel({ token }: { token: string }) {
  const [a, setA] = useState<SellerAnalytics | null>(null);
  useEffect(() => { getSellerAnalytics(token).then(setA).catch(() => {}); }, [token]);
  if (!a) return null;
  const money = (c: number) => `$${(c / 100).toLocaleString(undefined, { minimumFractionDigits: 2 })}`;
  return (
    <div className="card" style={{ marginTop: "1.5rem" }}>
      <h2 style={{ marginTop: 0 }}>Performance</h2>
      <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))" }}>
        <Stat n={`${Math.round(a.win_rate * 100)}%`} label={`Win rate (${a.quotes_won}/${a.quotes_submitted})`} />
        <Stat n={money(a.revenue_won_cents)} label="Revenue won" />
        <Stat n={a.avg_response_hours ? `${a.avg_response_hours.toFixed(1)}h` : "—"} label="Avg response" />
      </div>
      {a.top_buyers.length > 0 && (
        <>
          <h3>Top buyers</h3>
          <table className="tbl"><tbody>
            {a.top_buyers.map((b) => (
              <tr key={b.company_id}><td>{b.name}</td><td>{b.orders} order{b.orders === 1 ? "" : "s"}</td><td>{money(b.revenue_cents)}</td></tr>
            ))}
          </tbody></table>
        </>
      )}
    </div>
  );
}

function Stat({ n, label }: { n: string | number; label: string }) {
  return <div className="stat"><div className="n">{n}</div><div className="l">{label}</div></div>;
}
```

- [ ] **Step 2: Rewrite `web/app/(app)/dashboard/page.tsx`** to render real data by role. Keep the existing
  `Stat`/`Action` helpers style. Buyer branch uses `getBuyerDashboard`; seller branch uses
  `getSellerDashboard` + `<SellerAnalyticsPanel>`:
```tsx
"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { PageHeader } from "@/components/AppShell";
import { useMe } from "@/lib/auth";
import { getBuyerDashboard, getSellerDashboard, type BuyerDashboard, type SellerDashboard } from "@/lib/dashboard";
import { SellerAnalyticsPanel } from "@/components/SellerAnalytics";

export default function Dashboard() {
  const { token, me } = useMe();
  const [buyer, setBuyer] = useState<BuyerDashboard | null>(null);
  const [seller, setSeller] = useState<SellerDashboard | null>(null);

  const isSeller = me?.company_type === "seller" || me?.company_type === "both";
  const isBuyer = me ? me.company_type !== "seller" : false;

  useEffect(() => {
    if (!token || !me) return;
    if (isBuyer) getBuyerDashboard(token).then(setBuyer).catch(() => {});
    if (isSeller) getSellerDashboard(token).then(setSeller).catch(() => {});
  }, [token, me, isBuyer, isSeller]);

  if (!me) return null;

  return (
    <>
      <PageHeader title={`Welcome, ${me.company_name ?? "there"}`} subtitle="Your marketplace at a glance." />

      {isBuyer && buyer && (
        <section>
          <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))", marginBottom: "1rem" }}>
            <Stat n={buyer.active_rfqs} label="Active RFQs" />
            <Stat n={buyer.quotes_received} label="Quotes received" badge={buyer.new_quotes} />
            <Stat n={buyer.orders_in_progress} label="Orders in progress" />
          </div>
          <div className="card">
            <h2 style={{ marginTop: 0 }}>Recent RFQs</h2>
            {buyer.recent_rfqs.length === 0 ? <p className="muted">No RFQs yet.</p> : (
              <table className="tbl"><tbody>
                {buyer.recent_rfqs.map((r) => (
                  <tr key={r.id}>
                    <td><Link href={`/rfqs/${r.id}`}>{r.title}</Link></td>
                    <td><span className="badge">{r.status}</span></td>
                    <td>{r.quote_count} quote{r.quote_count === 1 ? "" : "s"}{r.new_quotes > 0 ? ` · ${r.new_quotes} new` : ""}</td>
                  </tr>
                ))}
              </tbody></table>
            )}
          </div>
        </section>
      )}

      {isSeller && seller && (
        <section style={{ marginTop: isBuyer ? "1.5rem" : 0 }}>
          <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))", marginBottom: "1rem" }}>
            <Stat n={seller.incoming_rfqs} label="Incoming RFQs" />
            <Stat n={seller.review_queue} label="Review queue" />
            <Stat n={seller.active_orders} label="Active orders" />
            <div className="stat">
              <div className="n" style={{ color: seller.agent.online ? "var(--ok, green)" : "var(--muted, gray)" }}>
                {seller.agent.online ? "Online" : "Offline"}
              </div>
              <div className="l">Agent{seller.agent.autonomy ? " · autonomous" : ""}</div>
            </div>
          </div>
          <div className="card">
            <h2 style={{ marginTop: 0 }}>Plan usage — {seller.plan_usage.plan}</h2>
            <p className="small">RFQs: {usage(seller.plan_usage.rfq_used, seller.plan_usage.rfq_limit)} · Catalog: {usage(seller.plan_usage.catalog_used, seller.plan_usage.catalog_limit)} · Seats: {usage(seller.plan_usage.seats_used, seller.plan_usage.seats_limit)}</p>
          </div>
          {token && <SellerAnalyticsPanel token={token} />}
        </section>
      )}
    </>
  );
}

function usage(used: number, limit: number): string {
  return limit < 0 ? `${used} / ∞` : `${used} / ${limit}`;
}

function Stat({ n, label, badge }: { n: number | string; label: string; badge?: number }) {
  return (
    <div className="stat">
      <div className="n">{n}{badge && badge > 0 ? <span className="badge" style={{ marginLeft: "0.4rem" }}>{badge} new</span> : null}</div>
      <div className="l">{label}</div>
    </div>
  );
}
```
> The `.stat .n/.l` classes are used by the existing dashboard — keep them. If `--ok`/`--muted` CSS vars are
> absent, the inline fallbacks (`green`/`gray`) apply. Verify `useMe` returns `company_name` on `me`
> (it does — `/v1/me` includes it).

- [ ] **Step 3:** `npx tsc --noEmit` clean; `npm run build` exits 0.
- [ ] **Step 4: Commit** — `feat(web): real buyer & seller dashboards + analytics panel`.

---

### Task 4: RFQ list tabs + search + new-quote badges

**Files:** `web/app/(app)/rfqs/page.tsx`.

- [ ] **Step 1:** Add tab + search state and a badge column to the RFQ list. Keep the existing "New request"
  form untouched; add above the list:
```tsx
  const [tab, setTab] = useState("");
  const [search, setSearch] = useState("");

  const refresh = () => {
    if (!token) return;
    listRfqs(token, { tab, q: search }).then((r) => setRfqs(r.rfqs)).catch((e) => setErr(e.message));
  };
  useEffect(() => {
    if (!token) return;
    listRfqs(token, { tab, q: search }).then((r) => setRfqs(r.rfqs)).catch((e) => setErr(e.message));
  }, [token, tab, search]);
```
  Render a tab bar + search box above the RFQ list, and show `new_quotes` as a badge on each row:
```tsx
      <div className="row" style={{ gap: "0.5rem", margin: "1rem 0", alignItems: "center", flexWrap: "wrap" }}>
        {[["", "All"], ["open", "Open"], ["quoted", "Quoted"], ["closed", "Closed"]].map(([val, label]) => (
          <button key={val} className="btn" aria-pressed={tab === val} onClick={() => setTab(val)}>{label}</button>
        ))}
        <input className="input" placeholder="Search RFQs…" value={search} onChange={(e) => setSearch(e.target.value)} style={{ flex: 1, minWidth: 180 }} />
      </div>
```
  In the RFQ rows, where the title/status renders, append:
```tsx
        {r.new_quotes && r.new_quotes > 0 ? <span className="badge">{r.new_quotes} new</span> : null}
        {typeof r.quote_count === "number" && <span className="muted small">{r.quote_count} quote{r.quote_count === 1 ? "" : "s"}</span>}
```
> Adapt to the existing list markup in `rfqs/page.tsx` (it maps `rfqs` to rows/links). Insert the badge +
> count near the RFQ title/status; do not restructure the whole list. The badge fields are optional and may
> be absent (treat absent as 0).

- [ ] **Step 2:** `npx tsc --noEmit` clean; `npm test` (unchanged libs still green); `npm run build` exits 0.
- [ ] **Step 3: Commit** — `feat(web): RFQ list tabs + search + new-quote badges`.

---

### Task 5: E2E #6 — discovery + dashboards render real aggregations

**Files:** `test/integration/e2e_06_discovery_dashboards_test.go` (Go).

**Interfaces:** reuses `buildAPI`, `buildGateway`, `registerSeller`, `doJSON`, `uniqueEmail`, `verifyUser`,
`poolOrSkip` from the existing integration harness.

- [ ] **Step 1: Write the spanning E2E.** Seed a verified seller (via the query layer), a matching buyer RFQ,
  a gateway quote, and an accepted order; then assert discovery + both dashboards + analytics on real data:
```go
package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// E2E #6 (Sprint 6): a buyer finds a supplier via public discovery; the buyer
// and seller dashboards + seller analytics render real aggregations after a
// full quote→accept→order flow.
func TestE2E06_DiscoveryAndDashboards(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	// Seller with a discoverable profile (registerSeller sets description
	// "cartons and pallets", categories ["packaging"]). Mark it verified so it
	// ranks and shows the badge.
	sellerToken, sellerCompany, secret := registerSeller(t, api, "E2E6Pack")
	require.NoError(t, q.SyncSupplierVerified(ctx, sqlc.SyncSupplierVerifiedParams{CompanyID: sellerCompany, Verified: true}))
	// Give the profile a distinctive search term.
	st, _ := doJSON(t, api, "PUT", "/v1/suppliers/me", sellerToken, map[string]any{
		"description": "e2e6probe cartons and pallets supplier", "categories": []string{"packaging"}, "location": "Dubai",
	})
	require.Equal(t, http.StatusOK, st)

	// Public discovery (no auth) finds the seller by need.
	st, disc := doJSON(t, api, "GET", "/public/v1/discovery?q=e2e6probe", "", nil)
	require.Equal(t, http.StatusOK, st)
	results := disc["results"].([]any)
	require.NotEmpty(t, results, "discovery finds the seller by need")
	found := false
	for _, r := range results {
		if r.(map[string]any)["company_id"] == sellerCompany {
			found = true
		}
	}
	require.True(t, found, "the pilot seller appears in discovery results")

	// Facets are non-empty for the query.
	st, facets := doJSON(t, api, "GET", "/public/v1/discovery/facets?q=e2e6probe", "", nil)
	require.Equal(t, http.StatusOK, st)
	require.NotEmpty(t, facets["categories"].([]any), "facets include categories")

	// Buyer posts a matching RFQ, seller agent quotes, buyer accepts → order.
	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "E2E6 Buyer", "company_type": "buyer",
		"email": uniqueEmail("e2e6-buyer-"), "password": "password123",
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
		"type": "quote.submit", "idempotency_key": "e2e6-submit", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1000, "currency": "USD", "delivery_days": 14, "notes": "in stock"},
	})
	require.Equal(t, http.StatusCreated, st)

	st, listed := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/quotes", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	quoteID := listed["quotes"].([]any)[0].(map[string]any)["id"].(string)

	st, accepted := doJSON(t, api, "POST", "/v1/quotes/"+quoteID+"/accept", buyerToken, nil)
	require.Equal(t, http.StatusCreated, st)
	_ = accepted

	// Buyer dashboard reflects the real flow.
	st, bd := doJSON(t, api, "GET", "/v1/dashboard/buyer", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.GreaterOrEqual(t, bd["quotes_received"].(float64), float64(1))
	require.GreaterOrEqual(t, bd["orders_in_progress"].(float64), float64(1))

	// Seller dashboard + analytics reflect the won deal.
	st, sd := doJSON(t, api, "GET", "/v1/dashboard/seller", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.GreaterOrEqual(t, sd["active_orders"].(float64), float64(1))

	st, an := doJSON(t, api, "GET", "/v1/analytics/seller", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.GreaterOrEqual(t, an["quotes_won"].(float64), float64(1))
	require.GreaterOrEqual(t, an["revenue_won_cents"].(float64), float64(100000))
	require.InDelta(t, 1.0, an["win_rate"].(float64), 0.001)
}
```
> Confirm the accept endpoint + quote-list shape match the Sprint-5 E2E (`e2e_05_counter_offer_test.go`):
> price 1000 → `price_cents` 100000 → order `total_cents` 100000, so `revenue_won_cents >= 100000`. If
> `registerSeller`'s returned tuple order differs, match it (it is `(token, companyID, secret)`).

- [ ] **Step 2: Run** the E2E then the full suite:
```bash
export PATH="$HOME/.local/bin:$PATH"
export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')
go test ./test/integration/ -run TestE2E06 -v
go test ./... -p 1
```
Expected: E2E passes; full suite green.

- [ ] **Step 3: Commit** — `test(core): E2E #6 — discovery + buyer/seller dashboards on real data`.

---

## Self-Review checklist

- Three lib clients + tests green (`npm test`); `tsc --noEmit` clean; `npm run build` exits 0 (never piped).
- Public marketplace + category pages work logged-out (no token), reuse VerifiedBadge/StarRating and existing
  CSS; facet sidebar filters; cards link to `/s/[company_id]`; matched-catalog shown when present.
- `/dashboard` renders real buyer tiles (active RFQs, quotes-received w/ new badge, orders in progress,
  recent list) and real seller tiles (incoming, review queue, active orders, agent online/autonomy, plan
  usage) + analytics panel (win rate, revenue, response time, top buyers).
- `/rfqs` has working open/quoted/closed tabs + search + per-row new-quote badge; existing create form intact.
- E2E #6 passes end-to-end through the real HTTP API; full Go suite green under `-p 1`; nothing pushed.
