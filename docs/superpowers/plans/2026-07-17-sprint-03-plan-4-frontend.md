# Sprint 3 — Plan 4: Frontend (Trust UI) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Buyer-app UI for Sprint 3 trust features — a verification settings page (claim domain, DNS record, verify-now), the verified badge on quotes, a public seller storefront, and report/block affordances.

**Architecture:** Next.js 16 / React 19 / TS strict in `core/web`. A new `lib/trust.ts` API client mirrors `lib/billing.ts` (own `call<T>` helper). Pages are client components using `useToken` (`@/lib/auth`) and `PageHeader` (`@/components/AppShell`). The public storefront lives OUTSIDE the `(app)` auth group (`app/s/[id]/`) so it's reachable unauthenticated; report/block controls appear only when a viewer token is present. Styling uses the existing global utility classes (`.card .col .field .input .btn .btn-primary .badge .badge-success .badge-warning .ok .err`).

**Tech Stack:** Next 16, React 19, TS strict, vitest (lib only — NO React Testing Library, per Sprint 1/2 precedent). Pages verified via `npx tsc --noEmit` + `npm run build`.

## Global Constraints

- Lib logic is unit-tested with vitest (mock `global.fetch`, mirroring `lib/billing.test.ts`). Pages/components are verified by `tsc --noEmit` + `npm run build` only.
- The public storefront (`/s/[id]`) must render for an unauthenticated viewer (no token) — it calls `getPublicSeller` with no auth. Report/Block controls render only when `useToken()` returns a token.
- The verified badge shows `✓ Verified` (green) for `verified`, `Pending` (amber) for `pending`, and nothing otherwise.
- Do not use `any`; keep TS strict clean. Match the existing minimal component style (see `app/(app)/seller/profile/page.tsx`).
- Backend endpoints (all built in Plans 1–3): `GET /v1/verification`, `POST /v1/verification/domain`, `POST /v1/verification/domain/check`, `GET /public/v1/sellers/:id`, `POST /v1/reports`, `GET/POST/DELETE /v1/blocks`, and `seller_verification_status` on `GET /v1/rfqs/:id/quotes`.

---

### Task 1: `lib/trust.ts` API client + tests

**Files:**
- Create: `web/lib/trust.ts`
- Create: `web/lib/trust.test.ts`
- Modify: `web/lib/api.ts` (add `seller_verification_status?` to `QuoteRow`)

**Interfaces:**
- Produces: types `VerificationStatus`, `VerificationState`, `Challenge`, `CheckResult`, `PublicSeller`, `BlockRow`; functions `getVerification`, `claimDomain`, `checkDomain`, `getPublicSeller`, `createReport`, `listBlocks`, `createBlock`, `removeBlock`.

- [ ] **Step 1: Write the failing test**

`web/lib/trust.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  getVerification, claimDomain, checkDomain, getPublicSeller,
  createReport, createBlock,
} from "./trust";

const realFetch = global.fetch;
beforeEach(() => { vi.restoreAllMocks(); });
afterEach(() => { global.fetch = realFetch; });

function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("trust client", () => {
  it("parses verification state", async () => {
    mockFetch(200, JSON.stringify({
      verification_status: "pending", verified_domain: null, verified_at: null,
      pending: [{ domain: "acme.com", record_name: "_tradebridge-verify.acme.com", record_type: "TXT", record_value: "tradebridge-verify=abc" }],
    }));
    const s = await getVerification("tok");
    expect(s.verification_status).toBe("pending");
    expect(s.pending[0].record_value).toContain("tradebridge-verify=");
  });

  it("claimDomain posts the domain and returns the record", async () => {
    mockFetch(200, JSON.stringify({ domain: "acme.com", record_name: "_tradebridge-verify.acme.com", record_type: "TXT", record_value: "tradebridge-verify=xyz" }));
    const ch = await claimDomain("tok", "acme.com");
    expect(ch.record_type).toBe("TXT");
    const init = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][1] as RequestInit;
    expect(JSON.parse(init.body as string)).toEqual({ domain: "acme.com" });
  });

  it("checkDomain returns verified flag", async () => {
    mockFetch(200, JSON.stringify({ status: "verified", verified: true }));
    const r = await checkDomain("tok", "acme.com");
    expect(r.verified).toBe(true);
  });

  it("getPublicSeller needs no token and returns safe fields", async () => {
    mockFetch(200, JSON.stringify({
      company_id: "c1", name: "Acme", verification_status: "verified", verified_domain: "acme.com",
      description: "widgets", categories: ["widgets"], location: "NYC",
      catalog: [{ name: "A", category: "widgets", summary: null }], reputation: null,
    }));
    const s = await getPublicSeller("c1");
    expect(s.name).toBe("Acme");
    expect(s.verification_status).toBe("verified");
    expect(s.catalog).toHaveLength(1);
    const init = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][1] as RequestInit;
    expect((init.headers as Record<string, string>).authorization).toBeUndefined();
  });

  it("createReport posts reason + detail", async () => {
    mockFetch(200, JSON.stringify({ id: "r1" }));
    const r = await createReport("tok", "c2", "spam", "spammy");
    expect(r.id).toBe("r1");
    const init = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][1] as RequestInit;
    expect(JSON.parse(init.body as string)).toEqual({ reported_company_id: "c2", reason: "spam", detail: "spammy" });
  });

  it("createBlock posts blocked id", async () => {
    mockFetch(200, JSON.stringify({ status: "blocked" }));
    const r = await createBlock("tok", "c2");
    expect(r.status).toBe("blocked");
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npm run test -- trust` (or `npx vitest run lib/trust.test.ts`)
Expected: FAIL (module not found).

- [ ] **Step 3: Implement `lib/trust.ts`**

`web/lib/trust.ts`:
```ts
import { CORE_URL } from "./api";

export type VerificationStatus = "unverified" | "pending" | "verified" | "rejected";

export type PendingClaim = {
  domain: string;
  record_name: string;
  record_type: string;
  record_value: string;
};
export type VerificationState = {
  verification_status: VerificationStatus;
  verified_domain: string | null;
  verified_at: string | null;
  pending: PendingClaim[];
};
export type Challenge = {
  domain: string;
  record_name: string;
  record_type: string;
  record_value: string;
};
export type CheckResult = { status: string; verified: boolean };
export type CatalogShowcaseItem = { name: string; category: string; summary: string | null };
export type PublicSeller = {
  company_id: string;
  name: string;
  verification_status: VerificationStatus;
  verified_domain: string | null;
  description: string | null;
  categories: string[];
  location: string | null;
  catalog: CatalogShowcaseItem[];
  reputation: null;
};
export type BlockRow = {
  id: string;
  blocked_company_id: string;
  blocked_name: string;
  created_at: string;
};

// Local request helper mirroring lib/billing.ts (Fiber returns JSON on success,
// plain text on error).
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
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return (text ? JSON.parse(text) : undefined) as T;
}

export const getVerification = (token: string) =>
  call<VerificationState>("GET", "/v1/verification", { token });

export const claimDomain = (token: string, domain: string) =>
  call<Challenge>("POST", "/v1/verification/domain", { token, body: { domain } });

export const checkDomain = (token: string, domain: string) =>
  call<CheckResult>("POST", "/v1/verification/domain/check", { token, body: { domain } });

export const getPublicSeller = (companyId: string) =>
  call<PublicSeller>("GET", `/public/v1/sellers/${companyId}`);

export const createReport = (token: string, reportedCompanyId: string, reason: string, detail: string) =>
  call<{ id: string }>("POST", "/v1/reports", {
    token,
    body: { reported_company_id: reportedCompanyId, reason, detail },
  });

export const listBlocks = (token: string) =>
  call<{ blocks: BlockRow[]; count: number }>("GET", "/v1/blocks", { token }).then((r) => r.blocks);

export const createBlock = (token: string, blockedCompanyId: string) =>
  call<{ status: string }>("POST", "/v1/blocks", { token, body: { blocked_company_id: blockedCompanyId } });

export const removeBlock = (token: string, blockedCompanyId: string) =>
  call<null>("DELETE", `/v1/blocks/${blockedCompanyId}`, { token });
```

- [ ] **Step 4: Add the quote field**

In `web/lib/api.ts`, add to the `QuoteRow` type:
```ts
  seller_verification_status?: string;
```

- [ ] **Step 5: Run the tests + typecheck**

Run: `cd web && npx vitest run lib/trust.test.ts && npx tsc --noEmit`
Expected: PASS; tsc clean.

- [ ] **Step 6: Commit**

```bash
git add web/lib/trust.ts web/lib/trust.test.ts web/lib/api.ts
git commit -m "feat(web): trust API client (verification, storefront, report/block) + quote badge field"
```

---

### Task 2: `<VerifiedBadge>` + surface on quotes

**Files:**
- Create: `web/components/VerifiedBadge.tsx`
- Modify: `web/app/(app)/rfqs/[id]/page.tsx` (badge next to seller name)

**Interfaces:**
- Produces: `<VerifiedBadge status={...} />`.

- [ ] **Step 1: Implement the component**

`web/components/VerifiedBadge.tsx`:
```tsx
// VerifiedBadge renders the seller trust badge. Verified → green check; pending
// → amber; anything else → nothing (unverified sellers show no badge).
export function VerifiedBadge({ status }: { status?: string | null }) {
  if (status === "verified") {
    return (
      <span className="badge badge-success" title="Domain-verified seller">
        ✓ Verified
      </span>
    );
  }
  if (status === "pending") {
    return (
      <span className="badge badge-warning" title="Verification pending">
        Pending
      </span>
    );
  }
  return null;
}
```

- [ ] **Step 2: Surface it on the quote table**

In `web/app/(app)/rfqs/[id]/page.tsx`:
- Import: `import { VerifiedBadge } from "@/components/VerifiedBadge";`
- In the seller-name cell (currently `<td>{q.seller_name ?? q.seller_company_id.slice(0, 8)}</td>`), add the badge:
```tsx
                <td>
                  {q.seller_name ?? q.seller_company_id.slice(0, 8)}{" "}
                  <VerifiedBadge status={q.seller_verification_status} />
                </td>
```

- [ ] **Step 3: Typecheck + build**

Run: `cd web && npx tsc --noEmit && npm run build`
Expected: clean build (do NOT pipe through `head` — SIGPIPE corrupts `.next`).

- [ ] **Step 4: Commit**

```bash
git add web/components/VerifiedBadge.tsx "web/app/(app)/rfqs/[id]/page.tsx"
git commit -m "feat(web): VerifiedBadge component + show on buyer quote comparison"
```

---

### Task 3: `/settings/verification` page + nav

**Files:**
- Create: `web/app/(app)/settings/verification/page.tsx`
- Modify: `web/components/AppShell.tsx` (nav entry)
- Modify: `web/app/(app)/seller/profile/page.tsx` (link to storefront + verification)

**Interfaces:**
- Consumes: `getVerification`, `claimDomain`, `checkDomain` (Task 1).

- [ ] **Step 1: Implement the page**

`web/app/(app)/settings/verification/page.tsx`:
```tsx
"use client";

import { useEffect, useState } from "react";
import { PageHeader } from "@/components/AppShell";
import { VerifiedBadge } from "@/components/VerifiedBadge";
import { useToken } from "@/lib/auth";
import { getVerification, claimDomain, checkDomain, type VerificationState, type Challenge } from "@/lib/trust";

export default function VerificationPage() {
  const token = useToken();
  const [state, setState] = useState<VerificationState | null>(null);
  const [domain, setDomain] = useState("");
  const [challenge, setChallenge] = useState<Challenge | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const refresh = () => {
    if (!token) return;
    getVerification(token).then(setState).catch(() => {});
  };
  useEffect(refresh, [token]);
  useEffect(() => {
    // Prefill the domain input from an existing pending claim.
    if (state?.pending?.length) {
      setDomain(state.pending[0].domain);
      setChallenge(state.pending[0]);
    }
  }, [state]);

  const claim = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true); setErr(null); setMsg(null);
    try {
      const ch = await claimDomain(token, domain);
      setChallenge(ch);
      setMsg("Add the DNS record below, then click Verify now. DNS can take a few minutes to propagate.");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "claim failed");
    } finally { setBusy(false); }
  };

  const verify = async () => {
    if (!token || !challenge) return;
    setBusy(true); setErr(null); setMsg(null);
    try {
      const r = await checkDomain(token, challenge.domain);
      if (r.verified) {
        setMsg("Verified! Your storefront and quotes now show the verified badge.");
        refresh();
      } else {
        setMsg("Not found yet — the TXT record isn't visible. Give DNS a few minutes and try again.");
      }
    } catch (e) {
      setErr(e instanceof Error ? e.message : "check failed");
    } finally { setBusy(false); }
  };

  const verified = state?.verification_status === "verified";

  return (
    <>
      <PageHeader title="Verification" subtitle="Prove you control your domain to earn the verified badge." />
      <div className="card" style={{ maxWidth: 640 }}>
        <div className="row" style={{ gap: "0.5rem", alignItems: "center", marginBottom: "0.6rem" }}>
          <strong>Status:</strong>
          <VerifiedBadge status={state?.verification_status} />
          <span className="muted">{state?.verification_status ?? "…"}</span>
        </div>
        {verified ? (
          <p className="ok">Verified domain: <strong>{state?.verified_domain}</strong></p>
        ) : (
          <form className="col" onSubmit={claim}>
            <div className="field">
              <label>Domain</label>
              <input className="input" value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="acme.com" />
            </div>
            <div><button className="btn btn-primary" disabled={busy || !domain}>{busy ? "Working…" : "Get DNS record"}</button></div>
          </form>
        )}
        {challenge && !verified && (
          <div style={{ marginTop: "1rem" }}>
            <p className="muted">Add this TXT record at your DNS provider:</p>
            <table className="tbl">
              <tbody>
                <tr><td><strong>Type</strong></td><td>{challenge.record_type}</td></tr>
                <tr><td><strong>Name</strong></td><td><code>{challenge.record_name}</code></td></tr>
                <tr><td><strong>Value</strong></td><td><code>{challenge.record_value}</code></td></tr>
              </tbody>
            </table>
            <div style={{ marginTop: "0.7rem" }}>
              <button className="btn btn-primary" onClick={verify} disabled={busy}>{busy ? "Checking…" : "Verify now"}</button>
            </div>
          </div>
        )}
        {msg && <p className="ok">{msg}</p>}
        {err && <p className="err">{err}</p>}
      </div>
    </>
  );
}
```
Note: if the global CSS lacks `.tbl`, `.row`, or `.muted`, use existing classes or inline styles (check `app/globals.css`; the codebase uses `.badge`, `.card`, `.col`, `.field`, `.input`, `.btn`, `.ok`, `.err`). Adjust to whatever exists — do not introduce broken class names.

- [ ] **Step 2: Add nav entry**

In `web/components/AppShell.tsx`, add to the Settings group's `items` array:
```tsx
      { href: "/settings/verification", label: "Verification", show: () => true },
```

- [ ] **Step 3: Link from the supplier profile page**

In `web/app/(app)/seller/profile/page.tsx`, add a line under the form (inside the `card` or after it) pointing to verification + the public storefront. Use `next/link`:
```tsx
        <p className="muted" style={{ marginTop: "0.8rem" }}>
          <Link href="/settings/verification">Verify your domain</Link> to earn the badge ·{" "}
          <Link href={`/s/${me?.company_id ?? ""}`}>View public storefront</Link>
        </p>
```
Add `import Link from "next/link";` and obtain `me` via the `useMe` hook (`@/lib/auth`) if not already present; if wiring `me` is awkward, link only to `/settings/verification` and omit the storefront link (keep it simple and type-clean).

- [ ] **Step 4: Typecheck + build**

Run: `cd web && npx tsc --noEmit && npm run build`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add "web/app/(app)/settings/verification/page.tsx" web/components/AppShell.tsx "web/app/(app)/seller/profile/page.tsx"
git commit -m "feat(web): verification settings page + nav + profile links"
```

---

### Task 4: Public storefront `/s/[id]`

**Files:**
- Create: `web/app/s/[id]/page.tsx`

**Interfaces:**
- Consumes: `getPublicSeller` (Task 1), `<VerifiedBadge>` (Task 2). Report/Block wired in Task 5.

- [ ] **Step 1: Implement the storefront page**

`web/app/s/[id]/page.tsx`:
```tsx
"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { VerifiedBadge } from "@/components/VerifiedBadge";
import { getPublicSeller, type PublicSeller } from "@/lib/trust";

export default function StorefrontPage() {
  const { id } = useParams<{ id: string }>();
  const [seller, setSeller] = useState<PublicSeller | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    getPublicSeller(id).then(setSeller).catch((e) => setErr(e instanceof Error ? e.message : "not found"));
  }, [id]);

  if (err) {
    return <main style={{ maxWidth: 760, margin: "3rem auto", padding: "0 1rem" }}><h1>Seller not found</h1><p className="muted">{err}</p></main>;
  }
  if (!seller) {
    return <main style={{ maxWidth: 760, margin: "3rem auto", padding: "0 1rem" }}><p className="muted">Loading…</p></main>;
  }

  return (
    <main style={{ maxWidth: 760, margin: "3rem auto", padding: "0 1rem" }}>
      <div className="row" style={{ gap: "0.6rem", alignItems: "center" }}>
        <h1 style={{ margin: 0 }}>{seller.name}</h1>
        <VerifiedBadge status={seller.verification_status} />
      </div>
      {seller.verified_domain && (
        <p className="muted">Verified domain: {seller.verified_domain}</p>
      )}

      <section className="card" style={{ marginTop: "1rem" }}>
        <h2 style={{ marginTop: 0 }}>About</h2>
        <p>{seller.description || "No description yet."}</p>
        <div className="row" style={{ gap: "0.4rem", flexWrap: "wrap" }}>
          {seller.categories.map((c) => <span key={c} className="badge">{c}</span>)}
        </div>
        {seller.location && <p className="muted" style={{ marginTop: "0.5rem" }}>{seller.location}</p>}
      </section>

      <section className="card" style={{ marginTop: "1rem" }}>
        <h2 style={{ marginTop: 0 }}>Catalog</h2>
        {seller.catalog.length === 0 ? (
          <p className="muted">No catalog items published.</p>
        ) : (
          <ul>
            {seller.catalog.map((it, i) => (
              <li key={i}><strong>{it.name}</strong> <span className="muted">({it.category})</span>{it.summary ? ` — ${it.summary}` : ""}</li>
            ))}
          </ul>
        )}
      </section>

      <section className="card" style={{ marginTop: "1rem" }}>
        <h2 style={{ marginTop: 0 }}>Reputation</h2>
        <p className="muted">Ratings & reviews coming soon.</p>
      </section>
    </main>
  );
}
```
Note: this matches the existing `web/app/(app)/rfqs/[id]/page.tsx`, which reads the route param via `const params = useParams<{ id: string }>(); const id = params.id;` — the same `useParams` hook is used here.

- [ ] **Step 2: Typecheck + build**

Run: `cd web && npx tsc --noEmit && npm run build`
Expected: clean; `/s/[id]` appears in the route list.

- [ ] **Step 3: Commit**

```bash
git add "web/app/s/[id]/page.tsx"
git commit -m "feat(web): public seller storefront page (badge, about, catalog, reputation slot)"
```

---

### Task 5: Report/Block affordances on the storefront

**Files:**
- Create: `web/components/ReportBlockActions.tsx`
- Modify: `web/app/s/[id]/page.tsx` (render the actions when a viewer token exists)

**Interfaces:**
- Consumes: `useToken`, `createReport`, `createBlock` (Task 1).

- [ ] **Step 1: Implement the component**

`web/components/ReportBlockActions.tsx`:
```tsx
"use client";

import { useState } from "react";
import { useToken } from "@/lib/auth";
import { createBlock, createReport } from "@/lib/trust";

const REASONS = ["spam", "fraud", "abuse", "inappropriate", "other"] as const;

// ReportBlockActions shows Block + Report controls for a seller. Renders nothing
// when no viewer is signed in (both actions require auth).
export function ReportBlockActions({ companyId }: { companyId: string }) {
  const token = useToken();
  const [showReport, setShowReport] = useState(false);
  const [reason, setReason] = useState<(typeof REASONS)[number]>("spam");
  const [detail, setDetail] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  if (!token) return null;

  const block = async () => {
    setBusy(true); setErr(null); setMsg(null);
    try {
      await createBlock(token, companyId);
      setMsg("Blocked. You won't be matched with this company.");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "block failed");
    } finally { setBusy(false); }
  };

  const submitReport = async () => {
    setBusy(true); setErr(null); setMsg(null);
    try {
      await createReport(token, companyId, reason, detail);
      setMsg("Report submitted. Our team will review it.");
      setShowReport(false);
      setDetail("");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "report failed");
    } finally { setBusy(false); }
  };

  return (
    <section className="card" style={{ marginTop: "1rem" }}>
      <div className="row" style={{ gap: "0.5rem" }}>
        <button className="btn" onClick={block} disabled={busy}>Block</button>
        <button className="btn" onClick={() => setShowReport((s) => !s)} disabled={busy}>Report</button>
      </div>
      {showReport && (
        <div className="col" style={{ marginTop: "0.7rem" }}>
          <div className="field">
            <label>Reason</label>
            <select className="input" value={reason} onChange={(e) => setReason(e.target.value as (typeof REASONS)[number])}>
              {REASONS.map((r) => <option key={r} value={r}>{r}</option>)}
            </select>
          </div>
          <div className="field">
            <label>Detail (optional)</label>
            <textarea className="input" rows={3} value={detail} onChange={(e) => setDetail(e.target.value)} />
          </div>
          <div><button className="btn btn-primary" onClick={submitReport} disabled={busy}>{busy ? "Submitting…" : "Submit report"}</button></div>
        </div>
      )}
      {msg && <p className="ok">{msg}</p>}
      {err && <p className="err">{err}</p>}
    </section>
  );
}
```

- [ ] **Step 2: Render it on the storefront**

In `web/app/s/[id]/page.tsx`:
- Import: `import { ReportBlockActions } from "@/components/ReportBlockActions";`
- After the Reputation section, add: `<ReportBlockActions companyId={seller.company_id} />`

- [ ] **Step 3: Typecheck + build**

Run: `cd web && npx tsc --noEmit && npm run build`
Expected: clean.

- [ ] **Step 4: Full lib test + build gate**

Run: `cd web && npm run test && npm run build`
Expected: all vitest suites green; production build succeeds with `/s/[id]` and `/settings/verification` in the route list.

- [ ] **Step 5: Commit**

```bash
git add web/components/ReportBlockActions.tsx "web/app/s/[id]/page.tsx"
git commit -m "feat(web): report/block affordances on the public storefront"
```

---

## Deferred / follow-ups

- Supplier-search UI (there is no search page today) — the badge field is ready in `lib/trust.ts` when one is built.
- Report/block 429 rate-limit toast handling — minor UX polish.
- Pretty storefront slugs (`/s/acme`).
- Admin UI for the reports/verification queues (uses the `/admin/v1/*` endpoints) — S8 admin console.
- Carrying an intended action through login on the public storefront (report/block currently just hide when logged out).
