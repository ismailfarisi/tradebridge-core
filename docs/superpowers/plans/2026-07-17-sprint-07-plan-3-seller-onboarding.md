# Sprint 7 · Plan 3 — Seller Onboarding + Activation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.
> Repo: `/home/deploy/b2b/tradebridge-core/web` (Next.js 16 / React 19 / TS strict). Commit per task.

**Goal:** Wire the Plan-2 primitives into the seller surfaces: the activation checklist on `/dashboard`,
purposeful empty states on the seller pages, upgrade nudges at real entitlement limits, and a deep link to
the agent-console conversational onboarding.

**Architecture:** Add `<ActivationChecklist>` to `/dashboard`; replace bare "no data" text with
`<EmptyState>` (always carrying a CTA — no dead-ends); add `<UpgradeNudge>` on catalog (near
`catalog_max_items`), team (near `seats`), and agent (locked `agent_autonomy`); add a "train your agent"
deep link driven by `NEXT_PUBLIC_AGENT_URL` (graceful when unset).

**Tech Stack:** Next.js client components, TS strict, existing CSS utilities. Verify via `tsc --noEmit` +
bare `npm run build` (never piped). No new lib tests (components aren't unit-tested in this repo).

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never push**.
- Verify from `web/`: `npm test` (still green), `npx tsc --noEmit`, bare `npm run build`.
- **Every `<EmptyState>` MUST pass a `cta`** (its no-dead-end guarantee is not type-enforced — Plan 2 review).
- Reuse Plan-2 components + `lib/billing.ts` (`getBillingSummary`, `usageLabel`). Do not add CSS frameworks.

## Reference (verified)

- `<ActivationChecklist>` (self-fetching, auto-hides at 100%), `<EmptyState title body? cta?>`,
  `<UpgradeNudge feature used? limit? planRequired? locked?>` from Plan 2 (`@/components/*`).
- `getBillingSummary(token) → {plan, entitlements:{agent_autonomy, catalog_max_items, rfq_monthly_limit,
  seats}, usage:{rfq_used,rfq_limit,catalog_used,catalog_limit,seats_used,seats_limit}}`.
- `/dashboard` (`app/(app)/dashboard/page.tsx`): has `useMe()`, `PageHeader`, buyer + seller sections;
  insert the checklist right after `<PageHeader .../>`.
- `/seller/catalog` (`app/(app)/seller/catalog/page.tsx`): `useToken`, `items` state, table with a
  `{items.length === 0 && <tr>…No items yet.</tr>}` row.
- `/seller/agent` (`app/(app)/seller/agent/page.tsx`): `useToken`, `creds`, issue form.
- `/settings/team` (`app/(app)/settings/team/page.tsx`): already has `getBillingSummary` `summary` +
  `UsageMeter` for seats.

## File Structure

- Modify: `web/app/(app)/dashboard/page.tsx` (checklist)
- Modify: `web/app/(app)/seller/catalog/page.tsx` (empty state + catalog nudge)
- Modify: `web/app/(app)/seller/agent/page.tsx` (empty state + agent-console link + autonomy nudge)
- Modify: `web/app/(app)/settings/team/page.tsx` (seats nudge)
- Create: `web/lib/agentConsole.ts` (deep-link helper)

---

### Task 1: Activation checklist on dashboard + seller empty states

**Files:** `web/app/(app)/dashboard/page.tsx`, `web/app/(app)/seller/catalog/page.tsx`,
`web/app/(app)/seller/agent/page.tsx`.

- [ ] **Step 1: Dashboard checklist.** In `dashboard/page.tsx` add the import
  `import { ActivationChecklist } from "@/components/ActivationChecklist";` and render it immediately after
  the `<PageHeader .../>` line (it self-fetches, is role-aware, and returns null at 100%):
```tsx
      <PageHeader title={`Welcome, ${me.company_name ?? "there"}`} subtitle="Your marketplace at a glance." />
      <ActivationChecklist />
```

- [ ] **Step 2: Catalog empty state.** In `seller/catalog/page.tsx` add
  `import { EmptyState } from "@/components/EmptyState";`. Replace the bare empty row
  `{items.length === 0 && <tr><td colSpan={4} className="muted">No items yet.</td></tr>}` so that when there
  are no items the table is replaced by an `<EmptyState>` (render it BELOW the table, or conditionally
  instead of the table). Simplest: keep the table but, when empty, render the empty state under it:
```tsx
            {items.length === 0 && (
              <tr><td colSpan={4}>
                <EmptyState
                  title="No catalog items yet"
                  body="Add products so buyers can find you and your agent can quote accurately."
                  cta={{ label: "Add your first item", href: "#add-item" }}
                />
              </td></tr>
            )}
```
  And add `id="add-item"` to the "Add item" card's wrapping element (the `<div className="card">` above the
  form) so the CTA anchors to it.

- [ ] **Step 3: Agent empty state.** In `seller/agent/page.tsx` add the EmptyState import and, in the
  credentials list card, when `creds.length === 0`, render:
```tsx
        {creds.length === 0 && (
          <EmptyState
            title="No agent connected yet"
            body="Issue a credential above, then start your seller agent with it to go live."
            cta={{ label: "How to connect", href: "#issue" }}
          />
        )}
```
  Add `id="issue"` to the "Issue a credential" card wrapper.

- [ ] **Step 4: Verify** `npx tsc --noEmit` clean; `npm run build` exits 0 (never piped).
- [ ] **Step 5: Commit** — `feat(web): activation checklist on dashboard + seller empty states`.

---

### Task 2: Upgrade nudges + agent-console deep link

**Files:** `web/lib/agentConsole.ts`, `web/app/(app)/seller/catalog/page.tsx`,
`web/app/(app)/settings/team/page.tsx`, `web/app/(app)/seller/agent/page.tsx`.

- [ ] **Step 1: `web/lib/agentConsole.ts`** — deep-link helper (graceful when the env var is unset):
```ts
// The seller agent console is a separate app (its URL is deploy-configured via
// NEXT_PUBLIC_AGENT_URL, baked at build time). When unset, callers should
// degrade to on-page guidance instead of a broken link.
export const AGENT_CONSOLE_URL = process.env.NEXT_PUBLIC_AGENT_URL ?? "";

export function agentOnboardingHref(): string | null {
  return AGENT_CONSOLE_URL ? `${AGENT_CONSOLE_URL.replace(/\/$/, "")}/onboarding` : null;
}
```

- [ ] **Step 2: Catalog nudge.** In `seller/catalog/page.tsx`, fetch billing usage and show a near-limit
  nudge above the items card. Add imports:
```tsx
import { UpgradeNudge } from "@/components/UpgradeNudge";
import { getBillingSummary, type BillingSummary } from "@/lib/billing";
```
  Add state + fetch (alongside the existing catalog fetch):
```tsx
  const [summary, setSummary] = useState<BillingSummary | null>(null);
  useEffect(() => {
    if (!token) return;
    getBillingSummary(token).then(setSummary).catch(() => {});
  }, [token]);
```
  Render above the "Items" card:
```tsx
      {summary && (
        <UpgradeNudge feature="catalog_max_items" used={summary.usage.catalog_used} limit={summary.usage.catalog_limit} />
      )}
```

- [ ] **Step 3: Seats nudge.** In `settings/team/page.tsx`, where `summary` already exists, render a
  `<UpgradeNudge>` near the seats `UsageMeter`:
```tsx
import { UpgradeNudge } from "@/components/UpgradeNudge";
// ...
          {summary && (
            <UpgradeNudge feature="seats" used={summary.usage.seats_used} limit={summary.usage.seats_limit} />
          )}
```
  (Place it right after the seats `UsageMeter` render.)

- [ ] **Step 4: Autonomy-locked nudge + agent-console link.** In `seller/agent/page.tsx`:
  - Add imports:
```tsx
import { UpgradeNudge } from "@/components/UpgradeNudge";
import { getBillingSummary, type BillingSummary } from "@/lib/billing";
import { agentOnboardingHref } from "@/lib/agentConsole";
```
  - Fetch the summary:
```tsx
  const [summary, setSummary] = useState<BillingSummary | null>(null);
  useEffect(() => {
    if (!token) return;
    getBillingSummary(token).then(setSummary).catch(() => {});
  }, [token]);
```
  - Below `<PageHeader .../>`, when the plan lacks autonomy, show the locked nudge:
```tsx
      {summary && !summary.entitlements.agent_autonomy && (
        <UpgradeNudge feature="agent_autonomy" locked planRequired="pro" />
      )}
```
  - Add a "Train your agent" card (deep link when configured, guidance otherwise):
```tsx
      <div className="card" style={{ marginBottom: "1.5rem" }}>
        <h2 style={{ marginTop: 0 }}>Train your agent</h2>
        <p className="muted small">Teach your agent your products, pricing, and terms so it quotes in your voice.</p>
        {agentOnboardingHref() ? (
          <a className="btn btn-primary" href={agentOnboardingHref() as string} target="_blank" rel="noopener noreferrer">
            Open agent onboarding
          </a>
        ) : (
          <p className="small">Open your seller agent console and run its onboarding to build its knowledge base.</p>
        )}
      </div>
```

- [ ] **Step 5: Verify** `npx tsc --noEmit` clean; `npm run build` exits 0 (never piped); `npm test` still
  green.
- [ ] **Step 6: Commit** — `feat(web): seller upgrade nudges (catalog/seats/autonomy) + agent-console link`.

---

## Self-Review checklist

- `<ActivationChecklist>` appears on `/dashboard` and drives sellers through profile→catalog→verify→agent→
  first-quote; hides at 100%.
- Every seller empty state (`catalog`, `agent`) is an `<EmptyState>` WITH a `cta` (no dead-ends), anchored to
  the relevant on-page action.
- Upgrade nudges appear only near real limits: catalog (`catalog_max_items`), seats (`seats`), and a locked
  `agent_autonomy` CTA when the plan lacks it — all opening the shared `UpgradeModal`.
- Agent-console link deep-links when `NEXT_PUBLIC_AGENT_URL` is set, else shows guidance (no broken link).
- `tsc --noEmit` clean; `npm run build` exits 0 (never piped); nothing pushed.

> **Publish note:** to activate the agent-console deep link on live, set
> `NEXT_PUBLIC_AGENT_URL=https://agent.switeaz.com` in `web/.env.local` before the web build.
