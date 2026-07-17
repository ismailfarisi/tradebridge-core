# Sprint 5 · Plan 1 — Quote Versioning + Counter-offer Loop Implementation Plan

> **For agentic workers:** TDD, commit per task. Follow `internal/quotes/service.go` (Record/Accept tx),
> `internal/gateway/commands.go` (command vocabulary + authz), `internal/orders` (error types).

**Goal:** Make quotes versioned + immutable-history, and add the counter-offer round-trip: a buyer
counters (`POST /v1/quotes/:id/counter`), which — when the seller has agent autonomy — reaches the
seller's agent as a `message.received` event; the agent replies with a `quote.counter` command that Core
validates and stores as a new immutable version, notifying the buyer.

**Architecture:** `quotes` stays the mutable **head** (all ranking/accept/invoice code keeps reading it);
a new immutable `quote_versions` table records every version. The buyer counter endpoint lives in the
quotes handler/service (uses the existing `s.pub` event publisher + a new entitlements check). The
`quote.counter` command is added to the gateway vocabulary. Migration 023.

**Tech Stack:** Go, Fiber, sqlc, pgx/v5, golang-migrate, Redis (via eventbus).

## Global Constraints

- Merge to `master` LOCALLY ONLY, never push.
- DB tests → `tradebridge_core_test` only. Derive URL secret-free:
  `TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')`
- Apply migration 023 to the test DB before DB tests (builds on ≤022, DB currently at v22).
- `sqlc generate` after query edits; keep `go build ./...` clean.
- sqlc struct: `quote_versions`→`sqlc.QuoteVersion`.
- Counter authorization: only the **buyer on the RFQ** may counter; only the **seller's agent** (matched
  to the RFQ, quote belongs to that seller) may `quote.counter`. Never trust client-set seller/quote ownership.

## File Structure

- Create: `pkg/db/migrations/023_quote_versions.up.sql`, `023_quote_versions.down.sql`
- Modify: `pkg/db/query/quotes.sql` (version queries; UpsertQuote returns version)
- Modify: `internal/quotes/service.go` (`Record` writes v1; new `RecordCounter`; counter service method)
- Modify: `internal/quotes/handler.go` (`POST /v1/quotes/:id/counter`)
- Modify: `internal/gateway/commands.go` (`quote.counter` in vocabulary + side effect)
- Modify: `internal/notify/notify.go` (+`TypeQuoteCountered`), `internal/notify/handler.go` (validEventType)
- Modify: `internal/httpapi/router.go` (pass entitlements to quotes service if needed)
- Create: `internal/quotes/counter_test.go` (DB), extend `internal/gateway/*_test.go`

## Migration 023

**`023_quote_versions.up.sql`:**
```sql
-- Quote versioning: `quotes` remains the mutable head (ranking/accept/invoice
-- read it); quote_versions is the immutable append-only history. version 1 is
-- the seller's initial submit; each counter appends version N+1.
ALTER TABLE quotes ADD COLUMN version INT NOT NULL DEFAULT 1;

CREATE TABLE quote_versions (
    id               TEXT PRIMARY KEY DEFAULT gen_ulid(),
    quote_id         TEXT NOT NULL REFERENCES quotes(id) ON DELETE CASCADE,
    version          INT  NOT NULL,
    price_cents      BIGINT NOT NULL,
    currency         TEXT NOT NULL DEFAULT 'USD',
    delivery_days    INT,
    notes            TEXT NOT NULL DEFAULT '',
    completeness     DOUBLE PRECISION NOT NULL DEFAULT 0,
    source           TEXT NOT NULL CHECK (source IN ('seller','counter')),
    agent_command_id TEXT REFERENCES agent_commands(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (quote_id, version)
);

CREATE INDEX idx_quote_versions_quote ON quote_versions (quote_id, version);
```

**`023_quote_versions.down.sql`:**
```sql
DROP TABLE IF EXISTS quote_versions;
ALTER TABLE quotes DROP COLUMN IF EXISTS version;
```

## Queries — add to `pkg/db/query/quotes.sql`

`UpsertQuote` already `RETURNING *` includes the new `version` column after regen — no change needed.

```sql
-- name: UpsertQuoteVersion :one
-- Idempotent per (quote_id, version): a seller resubmit keeps v1 current.
INSERT INTO quote_versions (quote_id, version, price_cents, currency, delivery_days, notes, completeness, source, agent_command_id)
VALUES (@quote_id, @version, @price_cents, @currency, @delivery_days, @notes, @completeness, @source, sqlc.narg(agent_command_id))
ON CONFLICT (quote_id, version) DO UPDATE
SET price_cents = EXCLUDED.price_cents, currency = EXCLUDED.currency,
    delivery_days = EXCLUDED.delivery_days, notes = EXCLUDED.notes,
    completeness = EXCLUDED.completeness, source = EXCLUDED.source,
    agent_command_id = EXCLUDED.agent_command_id
RETURNING *;

-- name: BumpQuoteHead :one
-- Applies a counter's terms to the head and increments the version atomically.
UPDATE quotes
SET price_cents = @price_cents, currency = @currency, delivery_days = @delivery_days,
    notes = @notes, completeness = @completeness, version = version + 1,
    status = 'submitted', updated_at = NOW()
WHERE id = @id
RETURNING *;

-- name: ListQuoteVersions :many
SELECT * FROM quote_versions WHERE quote_id = @quote_id ORDER BY version ASC;
```

## Notify addition

`internal/notify/notify.go`: add `TypeQuoteCountered = "quote.countered"`; add to `validEventType`.

---

### Task 1: Version 1 on submit + RecordCounter (service, DB)

**Files:** `internal/quotes/service.go`, `internal/quotes/counter_test.go`.

- Extend `Record` (used by `quote.submit`) so that after `UpsertQuote` it upserts the version-1 row:
```go
func Record(ctx context.Context, q *sqlc.Queries, rfqID, sellerCompanyID, commandID string, n Normalized) (sqlc.Quote, error) {
	var cmd *string
	if commandID != "" { cmd = &commandID }
	quote, err := q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfqID, SellerCompanyID: sellerCompanyID, AgentCommandID: cmd,
		PriceCents: n.PriceCents, Currency: n.Currency, DeliveryDays: n.DeliveryDays,
		Notes: n.Notes, Completeness: n.Completeness,
	})
	if err != nil { return sqlc.Quote{}, err }
	// Keep version 1 (the seller's current submit) in the immutable history.
	if _, err := q.UpsertQuoteVersion(ctx, sqlc.UpsertQuoteVersionParams{
		QuoteID: quote.ID, Version: quote.Version, PriceCents: quote.PriceCents, Currency: quote.Currency,
		DeliveryDays: quote.DeliveryDays, Notes: quote.Notes, Completeness: quote.Completeness,
		Source: "seller", AgentCommandID: cmd,
	}); err != nil { return sqlc.Quote{}, err }
	return quote, nil
}
```
- Add `RecordCounter` — applies the agent's counter to the head + appends the new version, in a tx:
```go
// RecordCounter applies a seller-agent counter to the quote head (new version)
// and appends the immutable version row. commandID is the quote.counter command.
func RecordCounter(ctx context.Context, q *sqlc.Queries, pool *pgxpool.Pool, quoteID, commandID string, n Normalized) (sqlc.Quote, sqlc.QuoteVersion, error) {
	tx, err := pool.Begin(ctx)
	if err != nil { return sqlc.Quote{}, sqlc.QuoteVersion{}, err }
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := q.WithTx(tx)
	head, err := qtx.BumpQuoteHead(ctx, sqlc.BumpQuoteHeadParams{
		ID: quoteID, PriceCents: n.PriceCents, Currency: n.Currency,
		DeliveryDays: n.DeliveryDays, Notes: n.Notes, Completeness: n.Completeness,
	})
	if err != nil { return sqlc.Quote{}, sqlc.QuoteVersion{}, err }
	var cmd *string
	if commandID != "" { cmd = &commandID }
	ver, err := qtx.UpsertQuoteVersion(ctx, sqlc.UpsertQuoteVersionParams{
		QuoteID: head.ID, Version: head.Version, PriceCents: head.PriceCents, Currency: head.Currency,
		DeliveryDays: head.DeliveryDays, Notes: head.Notes, Completeness: head.Completeness,
		Source: "counter", AgentCommandID: cmd,
	})
	if err != nil { return sqlc.Quote{}, sqlc.QuoteVersion{}, err }
	if err := tx.Commit(ctx); err != nil { return sqlc.Quote{}, sqlc.QuoteVersion{}, err }
	return head, ver, nil
}
```

- [ ] **Step 1: Write failing DB test** `counter_test.go` (harness like `internal/quotes/service_db_test.go`
  or `internal/orders/orders_db_test.go`). Seed rfq+seller, `Record` a v1 quote, then `RecordCounter`
  with new terms; assert head `version==2`, head price updated, `ListQuoteVersions` returns 2 rows with
  sources `["seller","counter"]` and ascending versions.
- [ ] **Step 2: Run, verify fail. Step 3: implement (regen sqlc first). Step 4: pass.**
- [ ] **Step 5: Commit** — `feat(quotes): quote versioning + RecordCounter (immutable history)`.

---

### Task 2: Buyer counter endpoint (+ message.received when seller has autonomy)

**Files:** `internal/quotes/service.go` (add `ent` dependency + `Counter` method), `internal/quotes/handler.go`,
`internal/httpapi/router.go`.

- Add `ent *entitlements.Service` to `quotes.Service` (final constructor param; update the router call site
  and any test constructors — grep `quotes.NewService(`).
- New service method:
```go
// Counter records a buyer's counter-offer message on the thread and, only when
// the seller has agent autonomy, emits a message.received event to the seller's
// agent carrying the proposed terms + thread. Returns the created message.
func (s *Service) Counter(ctx context.Context, buyerCompanyID, quoteID string, priceCents *int64, deliveryDays *int32, message string) (sqlc.QuoteMessage, error) {
	quote, err := s.q.GetQuote(ctx, quoteID)
	if errors.Is(err, pgx.ErrNoRows) { return sqlc.QuoteMessage{}, ErrNotFound }
	if err != nil { return sqlc.QuoteMessage{}, err }
	r, err := s.q.GetRFQ(ctx, quote.RfqID)
	if err != nil { return sqlc.QuoteMessage{}, err }
	if r.BuyerCompanyID != buyerCompanyID { return sqlc.QuoteMessage{}, ErrNotFound } // party-only, no leak
	// Human-readable body summarizing the ask.
	body := message
	// (append a terms summary to body when priceCents/deliveryDays set — see impl)
	msg, err := s.q.CreateQuoteMessage(ctx, sqlc.CreateQuoteMessageParams{
		QuoteID: quoteID, Author: "buyer", Body: body, Attachments: nil,
	})
	if err != nil { return sqlc.QuoteMessage{}, err }
	// Round-trip to the agent ONLY if the seller has agent autonomy.
	if s.ent != nil && s.ent.For(ctx, quote.SellerCompanyID).AgentAutonomy && s.pub != nil {
		payload, _ := json.Marshal(messageReceivedEvent{
			Type: "message.received", QuoteID: quoteID, RFQID: quote.RfqID,
			Counter: counterTerms{PriceCents: priceCents, DeliveryDays: deliveryDays},
			Thread:  buildThread(ctx, s.q, quoteID),
		})
		_ = s.pub.Publish(ctx, quote.SellerCompanyID, "message.received", payload)
	}
	return msg, nil
}
```
  (`ErrNotFound` is defined in the quotes package or mirror `orders`; check existing quotes errors and reuse.)
- Handler: `POST /v1/quotes/:id/counter` under the existing quotes group guard; parse
  `{price_cents *int64, delivery_days *int32, message string}`; map `ErrNotFound`→404; 200 `{message}`.
  Register the route in `Register` alongside the existing message routes.

- [ ] **Step 1: Write failing DB test**: with a seller whose plan has `AgentAutonomy=false`, `Counter`
  records a message but writes NO `agent_events` row of type `message.received` (query agent_events).
  With autonomy true (upgrade the seller's plan/subscription in the seed, or seed a plan row), it DOES.
  Also: a non-buyer caller → `ErrNotFound`.
- [ ] **Step 2–4: fail → implement (regen if needed) → pass.**
- [ ] **Step 5: Commit** — `feat(quotes): buyer counter endpoint + agent round-trip gated on autonomy`.

---

### Task 3: `quote.counter` gateway command

**Files:** `internal/gateway/commands.go`; extend a gateway test.

- Add `"quote.counter": true` to `supportedCommands`.
- `quote.counter` payload: `{quote_id, price, currency?, delivery_days?, notes?}`. Validation:
  - `quote_id` present; `GetQuote`; the quote's `RfqID` must match a matched RFQ for the agent's company
    (reuse the existing `RFQMatchExists` check — note the command carries `quote_id`, not `rfq_id`; derive
    `rfq_id` from the quote and run the same match check), and `quote.SellerCompanyID == agent.CompanyID`.
  - Normalize the terms with `quotes.Normalize` (reuse; it validates positive price).
- Side effect (after the command row is created): `quotes.RecordCounter(ctx, h.q, h.pool, quoteID, cmd.ID, n)`
  — **the gateway needs a `*pgxpool.Pool`**. Add `pool *pgxpool.Pool` as the FINAL param of
  `gateway.NewHandler` (current sig: `NewHandler(q, rdb, store, logger, rlc, notifier, ent)` in
  `internal/gateway/gateway.go`) and store it on `Handler`. The gateway is its OWN binary — pass
  `app.Pool` at its single construction site `cmd/gateway/main.go` (NOT `internal/httpapi/router.go`).
  Update any gateway test that builds a `Handler`. Then record a seller `quote_messages` row and notify
  the buyer:
```go
	h.notifier.NotifyCompany(ctx, buyerCompanyID, notify.Event{
		Type: notify.TypeQuoteCountered, Title: "Seller sent a counter-offer",
		Body: "The seller revised their quote.", Data: map[string]any{"rfq_id": rfqID, "quote_id": quoteID},
		CTALabel: "View thread", CTAPath: "/rfqs/" + rfqID,
	})
```
  (buyerCompanyID from the quote's RFQ.)
- Audit as with other commands.

- [ ] **Step 1: Write failing gateway test** (mirror `api_a4`-style, or a gateway unit test): an agent for
  the matched seller sends `quote.counter` for its quote → 201; head `version` incremented, a `counter`
  version row exists, buyer got a `quote.countered` notification. An agent NOT matched / not the quote's
  seller → 403.
- [ ] **Step 2–4: fail → implement → pass.**
- [ ] **Step 5: Commit** — `feat(gateway): quote.counter command → new immutable quote version + notify buyer`.

---

## Self-Review checklist

- `go build ./...` clean; full suite `go test ./... -p 1 -count=1` (migration 023 applied) — 0 failures.
- Migration 023 round-trips (down/up); `quotes.version` present; `quote_versions` UNIQUE(quote_id,version).
- Only the buyer-on-RFQ can counter; only the matched seller's agent can `quote.counter`; non-party→404, wrong-agent→403.
- `message.received` is emitted ONLY when the seller has agent autonomy.
- Head bump + version append are atomic (RecordCounter tx); accept/ranking/invoice still read the head correctly.
- No `%widgets%` supplier_profiles rows created in new tests (shared-DB flake hygiene).
