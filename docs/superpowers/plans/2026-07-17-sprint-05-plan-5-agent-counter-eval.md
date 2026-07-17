# Sprint 5 · Plan 5 — Agent Counter Reasoning + Golden-RFQ Eval Implementation Plan

> **For agentic workers:** This plan is in the **Agent repo** (`/home/deploy/b2b/tradebridge-agent`,
> Bun/TypeScript). Tests run via `bun test`. Commit per task. Follow `src/reasoning/handler.ts`
> (`handleQuoteMessage`) and `src/reasoning/quote.ts` (`generateQuote`/`heuristicQuote`).

**Goal:** (1) The seller's agent handles the new `message.received` (buyer counter) event by reasoning
over the thread + counter and replying with a `quote.counter` command. (2) A golden-RFQ eval harness with
a synthetic set guards the deterministic quote heuristic against regressions in CI.

**Architecture:** Add `"quote.counter"` to the `sendCommand` vocabulary; a new `handleCounter` in
`src/reasoning/handler.ts` (mirrors `handleQuoteMessage`) that maps a `generateQuote` decision to a
`quote.counter` command via a small pure `counterCommand()` helper; wire `runtime.ts` to route
`message.received`. Plus `src/eval/` — a fixture catalog + synthetic RFQs with expected quote ranges, run
through `heuristicQuote`, asserted by `test/eval.test.ts` and runnable via `bun run eval`.

**Tech Stack:** Bun, TypeScript, TypeORM (SQLite). No migration.

## Global Constraints

- Merge to `master` in the AGENT repo LOCALLY ONLY, never push. Agent `.env`/secrets never committed.
- `cd /home/deploy/b2b/tradebridge-agent`; verify with `bun test` (whole suite green) + `bunx tsc --noEmit` if configured.
- Tests must NOT require the LLM (no API key) or network. Prefer DB-free tests: unit-test the pure
  `counterCommand()` mapping and the eval's `heuristicQuote` path directly (no TypeORM DataSource needed).
- The `quote.counter` command payload carries `quote_id` (Core derives the RFQ from it) — do NOT send `rfqId`.

## Backend contract (Core, shipped Plan 1)

- Core emits event `message.received` (to the seller's agent, only when the seller has agent autonomy)
  with payload: `{quote_id, rfq_id, counter:{price_cents, delivery_days}, thread:[{author,body}]}`.
- Core accepts command `quote.counter` at `POST /agent/v1/commands`:
  `{type:"quote.counter", idempotency_key, payload:{quote_id, price, currency, delivery_days?, notes?}}`.
  Core validates the agent owns the quote + is matched to its RFQ, then appends an immutable version.

## File Structure

- Modify: `src/core/commands.ts` (add `"quote.counter"` to the `Command.type` union)
- Modify: `src/reasoning/handler.ts` (add `counterCommand()` helper + `handleCounter()`)
- Modify: `src/runtime.ts` (route `message.received` → `handleCounter`)
- Create: `src/eval/fixtures.ts` (fixture catalog + synthetic RFQs + expected ranges)
- Create: `src/eval/run.ts` (runner: runs each fixture through `heuristicQuote`, returns pass/fail report)
- Create: `test/eval.test.ts` (asserts every fixture passes)
- Modify: `package.json` (add `"eval": "bun run src/eval/run.ts"` script)
- Create: `test/counter-command.test.ts` (pure mapping unit test)

---

### Task 1: `quote.counter` command + counter reasoning handler

**Files:** `src/core/commands.ts`, `src/reasoning/handler.ts`, `src/runtime.ts`, `test/counter-command.test.ts`.

- `src/core/commands.ts`: extend the union:
```ts
export interface Command {
  type: "quote.submit" | "clarification.request" | "no_bid" | "quote.counter";
  idempotencyKey: string;
  rfqId?: string;
  payload?: Record<string, unknown>;
}
```
- `src/reasoning/handler.ts` — a **pure, testable** mapping helper:
```ts
import type { QuoteDecision } from "./quote.ts";
import type { Command } from "../core/commands.ts";

// counterCommand maps a reasoning decision to a quote.counter command for the
// buyer's counter on quoteId, or null when the agent can't price a counter
// (a clarification decision — the agent should not counter with a question).
export function counterCommand(quoteId: string, eventId: string, decision: QuoteDecision): Command | null {
  if (decision.kind !== "quote") return null;
  const q = decision.quote;
  return {
    type: "quote.counter",
    idempotencyKey: `counter-${quoteId}-${eventId}`,
    payload: {
      quote_id: quoteId,
      price: q.price,
      currency: q.currency,
      delivery_days: q.deliveryDays,
      notes: q.notes,
    },
  };
}
```
- `handleCounter` (mirror `handleQuoteMessage`): parse `{quote_id, rfq_id, counter, thread}`; look up the
  `Rfq` row by `rfq_id` (skip if unknown / not active / replay via `lastThreadEventId === eventId`); append
  the buyer's counter to the thread as a `ThreadMsg` (`{author:"buyer", body}` — summarize `counter` terms
  into the body, e.g. `"Counter: <price_cents/100> <currency>, <delivery_days>d — <message>"`);
  `generateQuote({sellerId, rfqId, title, requirements, thread, mustQuote:true})`; then
  `const cmd = counterCommand(quote_id, eventId, decision)`; if `cmd`, `sendCommand(deps.coreUrl, deps.token, cmd)`
  and update `lastThreadEventId`; log the outcome. `mustQuote:true` so the agent prices rather than asks.
  (Autonomy: Core only emits `message.received` to entitled sellers, so no extra gate is needed here; keep
  it simple — do not re-run the autonomy policy.)
- `src/runtime.ts`: add a branch:
```ts
	} else if (ev.type === "message.received" && deps.autoQuote) {
		await handleCounter(ev.data, { coreUrl: deps.coreUrl, token: deps.token }, ev.id);
	}
```
  (`ev.data` is the raw JSON string — passed directly, exactly like `handleQuoteMessage(ev.data, …)`;
  `handleCounter` does `JSON.parse` internally.)

- [ ] **Step 1: Failing test** `test/counter-command.test.ts` (pure, no DB/LLM):
  `counterCommand("q1","e1",{kind:"quote",quote:{price:42.5,currency:"USD",deliveryDays:7,notes:"ok"}})`
  → `{type:"quote.counter", idempotencyKey:"counter-q1-e1", payload:{quote_id:"q1",price:42.5,currency:"USD",delivery_days:7,notes:"ok"}}`;
  `counterCommand("q1","e1",{kind:"clarification",question:"?"})` → `null`.
- [ ] **Step 2: Run** `bun test test/counter-command.test.ts` → fail. **Step 3: implement. Step 4: pass.**
- [ ] **Step 5:** `bun test` (whole suite) green; commit — `feat(agent): quote.counter command + message.received counter handler`.

---

### Task 2: Golden-RFQ eval harness + synthetic set

**Files:** `src/eval/fixtures.ts`, `src/eval/run.ts`, `test/eval.test.ts`, `package.json`.

Note: `heuristicQuote(requirements, items, capacityDays)` (in `src/reasoning/quote.ts`) reads each item's
**unit price from its `summary`** via a `price: $N` regex, and prices the cheapest matching item ×
quantity. Build fixtures accordingly (no DB, no LLM).

- `src/eval/fixtures.ts`:
```ts
import type { CatalogItem } from "../db/entities/CatalogItem.ts";
// Minimal catalog items (only the fields heuristicQuote reads): name, category, summary (with "price: $N").
export const FIXTURE_CATALOG = [
  { name: "Cardboard box 30cm", category: "packaging", summary: "Sturdy shipping box. price: $2.00" },
  { name: "Steel bolt M8",      category: "fasteners", summary: "Grade 8.8 hex bolt. price: $0.25" },
  // …10–20 total across a few categories…
] as unknown as CatalogItem[];

export interface EvalCase {
  name: string;
  requirements: { category?: string; quantity?: string; keywords?: string[] };
  expect: { priceMin: number; priceMax: number; deliveryMax: number };
}
export const EVAL_CASES: EvalCase[] = [
  { name: "500 boxes", requirements: { category: "packaging", quantity: "500" }, expect: { priceMin: 900, priceMax: 1100, deliveryMax: 30 } },
  // … one per priceable fixture, ranges derived from unit×qty with a tolerance …
];
```
- `src/eval/run.ts`:
```ts
import { heuristicQuote } from "../reasoning/quote.ts";
import { FIXTURE_CATALOG, EVAL_CASES } from "./fixtures.ts";

export interface EvalResult { name: string; ok: boolean; detail: string }
export function runEval(): EvalResult[] {
  return EVAL_CASES.map((c) => {
    const d = heuristicQuote(c.requirements, FIXTURE_CATALOG, null);
    if (d.kind !== "quote") return { name: c.name, ok: false, detail: `expected a quote, got ${d.kind}` };
    const p = d.quote.price, dd = d.quote.deliveryDays ?? 0;
    const ok = p >= c.expect.priceMin && p <= c.expect.priceMax && dd <= c.expect.deliveryMax;
    return { name: c.name, ok, detail: `price=${p} delivery=${dd}` };
  });
}
// CLI entrypoint: print a report and exit non-zero on any failure (fails CI).
if (import.meta.main) {
  const results = runEval();
  for (const r of results) console.log(`${r.ok ? "PASS" : "FAIL"}  ${r.name}  ${r.detail}`);
  const failed = results.filter((r) => !r.ok).length;
  console.log(`\n${results.length - failed}/${results.length} passed`);
  if (failed > 0) process.exit(1);
}
```
- `test/eval.test.ts`:
```ts
import { expect, test } from "bun:test";
import { runEval } from "../src/eval/run.ts";
test("golden-RFQ eval: every case within its expected quote range", () => {
  const results = runEval();
  const failures = results.filter((r) => !r.ok);
  expect(failures, JSON.stringify(failures, null, 2)).toHaveLength(0);
});
```
- `package.json` scripts: add `"eval": "bun run src/eval/run.ts"`.

- [ ] **Step 1:** Write `fixtures.ts` with 10–20 priceable cases; write `run.ts` + `test/eval.test.ts`.
  Calibrate each `expect` range to what `heuristicQuote` actually returns (run `bun run eval` and tighten
  the ranges around the real output with a sensible tolerance — the point is to catch *future* drift, so
  ranges should be tight enough to fail on a formula change but not flaky).
- [ ] **Step 2: Run** `bun run eval` → all PASS, exit 0; `bun test test/eval.test.ts` → green.
- [ ] **Step 3:** `bun test` (whole suite) green.
- [ ] **Step 4: Commit** — `feat(agent): golden-RFQ eval harness + synthetic set (CI quote-quality gate)`.

- [ ] **Step 5:** Add a short `src/eval/README.md` noting the set is synthetic and how to swap in real
  pilot catalogs later; commit — `docs(agent): eval harness README`.

---

## Self-Review checklist

- `bun test` (whole agent suite) green; `bun run eval` exits 0 with all PASS.
- `counterCommand` maps a quote decision → `quote.counter` (with `quote_id`, no `rfqId`) and a clarification → null.
- `runtime.ts` routes `message.received` → `handleCounter`; replay-safe via `lastThreadEventId`.
- No test requires an API key or network (stub/heuristic + pure functions only).
- The eval fails (non-zero exit) if a fixture falls out of range — verify by temporarily loosening a fixture price and confirming it flags (then revert).
