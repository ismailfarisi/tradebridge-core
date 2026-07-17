# Sprint 7 · Plan 4 — Buyer Onboarding + Activation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.
> Repo: `/home/deploy/b2b/tradebridge-core/web` (Next.js 16 / React 19 / TS strict). Commit per task.

**Goal:** Guide new buyers to their first posted RFQ: a "what do you buy" primer + prefilled sample-RFQ
templates in `/chat` (nothing persisted), purposeful empty states on `/rfqs` and `/orders`, and a buyer
RFQ-limit upgrade nudge.

**Architecture:** Enhance the `/chat` empty state with primer copy + clickable example prompts that prefill
the composer draft (`setDraft`, no persistence). Replace bare "no data" rows on `/rfqs` and `/orders` with
`<EmptyState>` (always with a CTA). Add `<UpgradeNudge feature="rfq_monthly_limit">` on `/rfqs` for buyers
near their monthly RFQ cap. (The buyer activation checklist is already on `/dashboard` from Plan 3 — it is
role-aware and shows the buyer steps automatically; the assist paywall is already handled reactively by
`AssistImprove` on 402.)

**Tech Stack:** Next.js client components, TS strict, existing CSS utilities. Verify via `tsc --noEmit` +
bare `npm run build`. No new lib tests.

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never push**.
- Verify from `web/`: `npm test` (green), `npx tsc --noEmit`, bare `npm run build` (never piped).
- **Every `<EmptyState>` MUST pass a `cta`** (no dead-ends).
- Sample templates PREFILL the composer only — never auto-submit and never persist seed rows.
- Reuse Plan-2 `<EmptyState>`/`<UpgradeNudge>` and `lib/billing.ts` `getBillingSummary`.

## Reference (verified)

- `/chat` (`app/(chat)/chat/page.tsx`): `useToken`, `draft`/`setDraft`, `state.messages`,
  `const empty = state.messages.length === 0`, and an empty-state block ("What do you need to source?").
- `/rfqs` (`app/(app)/rfqs/page.tsx`): `useToken`, `rfqs` state, `tab`/`search`, bare empty row
  `{rfqs.length === 0 && <tr>…No RFQs yet.</tr>}`.
- `/orders` (`app/(app)/orders/page.tsx`): `orders` state, bare empty row `{orders.length === 0 && <tr>…No
  orders yet.</tr>}`.
- `getBillingSummary(token) → {usage:{rfq_used,rfq_limit,...}}`.

## File Structure

- Modify: `web/app/(chat)/chat/page.tsx` (primer + sample templates)
- Modify: `web/app/(app)/rfqs/page.tsx` (empty state + RFQ-limit nudge)
- Modify: `web/app/(app)/orders/page.tsx` (empty state)

---

### Task 1: Chat first-RFQ primer + prefilled sample templates

**Files:** `web/app/(chat)/chat/page.tsx`.

- [ ] **Step 1:** Add a module-level constant of example RFQ prompts (above the component):
```tsx
const SAMPLE_RFQS = [
  "Need 500 corrugated shipping cartons (30×30×30cm), delivered to Dubai within 3 weeks.",
  "Looking for 1,000 stainless steel M8 bolts, grade 8.8 — quote per unit and lead time.",
  "Sourcing 200 reams of A4 80gsm copy paper for a Riyadh office, recurring monthly.",
];
```

- [ ] **Step 2:** Replace the existing empty-state block (the `{empty ? (…) : (…)}` "What do you need to
  source?" branch) so the empty state also shows a primer + clickable sample templates that prefill the
  composer draft. Keep the existing heading; add below it:
```tsx
          <div style={{ textAlign: "center", marginTop: "12vh" }}>
            <h1 style={{ fontSize: "1.5rem" }}>What do you need to source?</h1>
            <p className="muted" style={{ marginTop: "0.5rem" }}>
              Describe it in plain language — I&apos;ll match sellers and collect quotes. Include quantity,
              specs, destination, and any deadline.
            </p>
            <div className="col" style={{ gap: "0.5rem", maxWidth: 560, margin: "1.5rem auto 0" }}>
              <p className="muted small" style={{ margin: 0 }}>Try an example:</p>
              {SAMPLE_RFQS.map((s) => (
                <button
                  key={s}
                  type="button"
                  className="card"
                  onClick={() => setDraft(s)}
                  style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--border)", background: "var(--surface)" }}
                >
                  <span className="small">{s}</span>
                </button>
              ))}
            </div>
          </div>
```
> This only calls `setDraft(s)` — it fills the composer so the user reviews/edits then submits. Nothing is
> posted or persisted on click. Confirm `setDraft` is in scope (it is: `const [draft, setDraft] = useState("")`).

- [ ] **Step 3:** Verify `npx tsc --noEmit` clean; `npm run build` exits 0 (never piped). Manually confirm
  clicking a sample fills the composer (the `Composer` reads `draft`).
- [ ] **Step 4: Commit** — `feat(web): buyer first-RFQ primer + prefilled sample templates in chat`.

---

### Task 2: Buyer empty states + RFQ-limit nudge

**Files:** `web/app/(app)/rfqs/page.tsx`, `web/app/(app)/orders/page.tsx`.

- [ ] **Step 1: RFQs empty state + nudge.** In `rfqs/page.tsx` add imports:
```tsx
import { EmptyState } from "@/components/EmptyState";
import { UpgradeNudge } from "@/components/UpgradeNudge";
import { getBillingSummary, type BillingSummary } from "@/lib/billing";
```
  Add billing state + fetch:
```tsx
  const [billing, setBilling] = useState<BillingSummary | null>(null);
  useEffect(() => {
    if (!token) return;
    getBillingSummary(token).then(setBilling).catch(() => {});
  }, [token]);
```
  Render the nudge near the top of the list area (before the table/tabs):
```tsx
      {billing && (
        <UpgradeNudge feature="rfq_monthly_limit" used={billing.usage.rfq_used} limit={billing.usage.rfq_limit} />
      )}
```
  Replace the bare empty row. When `rfqs.length === 0`, distinguish "no RFQs at all" from "none match the
  filter". Simplest — replace the `<tr>…No RFQs yet.</tr>` with an EmptyState whose copy adapts:
```tsx
            {rfqs.length === 0 && (
              <tr><td colSpan={5}>
                <EmptyState
                  title={tab || search ? "No RFQs match this filter" : "No RFQs yet"}
                  body={tab || search ? "Try a different tab or clear your search." : "Post your first request and Core will match sellers and collect quotes."}
                  cta={tab || search ? { label: "Clear filters", href: "/rfqs" } : { label: "Post an RFQ", href: "/chat" }}
                />
              </td></tr>
            )}
```
> `href: "/rfqs"` for "clear filters" is acceptable (navigates to the unfiltered list). If the columns count
> differs from 5, use the real `colSpan`.

- [ ] **Step 2: Orders empty state.** In `orders/page.tsx` add
  `import { EmptyState } from "@/components/EmptyState";` and replace the bare empty row
  `{orders.length === 0 && <tr>…No orders yet.</tr>}`:
```tsx
            {orders.length === 0 && (
              <tr><td colSpan={5}>
                <EmptyState
                  title="No orders yet"
                  body="When you accept a quote, the purchase order shows up here."
                  cta={{ label: "Browse suppliers", href: "/marketplace" }}
                />
              </td></tr>
            )}
```
> Confirm the table's real `colSpan` (the existing empty row uses `colSpan={5}`); match it.

- [ ] **Step 3: Verify** `npx tsc --noEmit` clean; `npm run build` exits 0 (never piped); `npm test` green.
- [ ] **Step 4: Commit** — `feat(web): buyer empty states on rfqs/orders + RFQ-limit nudge`.

---

## Self-Review checklist

- `/chat` empty state has a primer (quantity/specs/destination/deadline guidance) + 3 clickable sample
  prompts that PREFILL the composer via `setDraft` (no submit, no persistence).
- `/rfqs` and `/orders` empty states are `<EmptyState>` WITH a `cta` (no dead-ends); the RFQs empty copy
  adapts to whether a tab/search filter is active.
- The buyer RFQ-limit nudge (`rfq_monthly_limit`) appears only near the real limit (via `shouldNudge`).
- Buyer activation checklist already present on `/dashboard` (Plan 3, role-aware) — no duplicate here.
- `tsc --noEmit` clean; `npm run build` exits 0 (never piped); nothing pushed.
