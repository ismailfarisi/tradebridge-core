# Sprint 4 · Plan 2 — Invoicing Implementation Plan

> **For agentic workers:** TDD, commit per task. Follow `internal/trust` (service split) and
> `internal/quotes/service.go` (transactional Accept) patterns.

**Goal:** When a buyer accepts a quote, atomically create an invoice (human-readable number, line
item derived from the RFQ) alongside the order, expose a downloadable PDF at
`GET /v1/orders/:id/invoice.pdf`, and email the buyer that the invoice was issued.

**Architecture:** New `internal/invoicing` package with a `Service` (create invoice + line item using a
caller-supplied tx-bound `*sqlc.Queries`, and read helpers) and a pure `Renderer` (go-pdf/fpdf → PDF
bytes). The existing `quotes.Service.Accept()` calls `invoicing.CreateForOrder` **inside its
transaction** so an order always has exactly one invoice; after commit it notifies the buyer.
Migration 020 adds `invoices`, `invoice_line_items`, and an `invoice_number_seq` sequence.

**Tech Stack:** Go, Fiber, sqlc, pgx/v5, `github.com/go-pdf/fpdf` (v0.9.0, already in module cache),
golang-migrate.

## Global Constraints

- Merge to `master` LOCALLY ONLY, never push.
- DB tests → `tradebridge_core_test` only. Derive URL secret-free:
  `TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')`
- Apply migration 020 to the test DB before DB tests (`migrate ... up`). It builds on 019 (Plan 1).
- `sqlc generate` after editing queries. `go build ./...` must stay clean.
- Add the dep with the module already cached: `GOFLAGS=-mod=mod go get github.com/go-pdf/fpdf@v0.9.0` (or just import it and run `go mod tidy`).
- sqlc struct names: `invoices`→`sqlc.Invoice`, `invoice_line_items`→`sqlc.InvoiceLineItem`, `rfqs`→`sqlc.Rfq`.
- `amount_cents` on the line item is the authoritative money figure; `unit_price_cents` is display-only.

## File Structure

- Create: `pkg/db/migrations/020_invoicing.up.sql`, `020_invoicing.down.sql`
- Create: `pkg/db/query/invoicing.sql`
- Create: `internal/invoicing/service.go` (CreateForOrder + read helpers + line-item/number helpers)
- Create: `internal/invoicing/renderer.go` (fpdf → []byte)
- Create: `internal/invoicing/handler.go` (GET /v1/orders/:id/invoice.pdf)
- Create: `internal/invoicing/service_test.go` (helpers + DB), `internal/invoicing/renderer_test.go`
- Modify: `internal/quotes/service.go` (inject invoicing; create invoice in Accept tx; notify buyer)
- Modify: `internal/quotes/*_test.go` (construct quotes.Service with the new invoicing dependency)
- Modify: `internal/notify/notify.go` (+`TypeInvoiceIssued`), `internal/notify/handler.go` (validEventType)
- Modify: `internal/httpapi/router.go` (build invoicing.Service, pass to quotes.NewService, register handler)

## Migration 020

**`020_invoicing.up.sql`:**
```sql
-- Invoicing: one invoice per order, generated at quote acceptance. invoice_number
-- is human-readable and globally unique, drawn from a sequence: INV-<YYYY>-<000001>.
CREATE SEQUENCE IF NOT EXISTS invoice_number_seq;

CREATE TABLE invoices (
    id                TEXT PRIMARY KEY DEFAULT gen_ulid(),
    order_id          TEXT NOT NULL UNIQUE REFERENCES orders(id) ON DELETE CASCADE,
    invoice_number    TEXT NOT NULL UNIQUE,
    buyer_company_id  TEXT NOT NULL REFERENCES companies(id),
    seller_company_id TEXT NOT NULL REFERENCES companies(id),
    currency          TEXT NOT NULL DEFAULT 'USD',
    subtotal_cents    BIGINT NOT NULL,
    total_cents       BIGINT NOT NULL,
    issued_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE invoice_line_items (
    id               TEXT PRIMARY KEY DEFAULT gen_ulid(),
    invoice_id       TEXT NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    description      TEXT NOT NULL,
    quantity         BIGINT NOT NULL DEFAULT 1,
    unit_price_cents BIGINT NOT NULL,
    amount_cents     BIGINT NOT NULL
);

CREATE INDEX idx_invoice_line_items_invoice ON invoice_line_items (invoice_id);
```

**`020_invoicing.down.sql`:**
```sql
DROP TABLE IF EXISTS invoice_line_items;
DROP TABLE IF EXISTS invoices;
DROP SEQUENCE IF EXISTS invoice_number_seq;
```

## Queries — `pkg/db/query/invoicing.sql`

```sql
-- name: CreateInvoice :one
-- invoice_number is generated in SQL so it is atomic with the row insert.
INSERT INTO invoices (order_id, invoice_number, buyer_company_id, seller_company_id, currency, subtotal_cents, total_cents)
VALUES (
    @order_id,
    'INV-' || to_char(NOW(), 'YYYY') || '-' || lpad(nextval('invoice_number_seq')::text, 6, '0'),
    @buyer_company_id, @seller_company_id, @currency, @subtotal_cents, @total_cents
)
RETURNING *;

-- name: CreateInvoiceLineItem :one
INSERT INTO invoice_line_items (invoice_id, description, quantity, unit_price_cents, amount_cents)
VALUES (@invoice_id, @description, @quantity, @unit_price_cents, @amount_cents)
RETURNING *;

-- name: GetInvoiceByOrder :one
SELECT * FROM invoices WHERE order_id = @order_id;

-- name: ListInvoiceLineItems :many
SELECT * FROM invoice_line_items WHERE invoice_id = @invoice_id ORDER BY id;
```

## Notify addition

`internal/notify/notify.go`: add `TypeInvoiceIssued = "invoice.issued"` to the const block (with a
doc comment). Add it to `validEventType` in `internal/notify/handler.go`.

---

### Task 1: Line-item derivation + invoice service (helpers, then DB)

**Files:** `internal/invoicing/service.go`, `internal/invoicing/service_test.go`.

**Interfaces (Produces):**
- `type Service struct{ logger zerolog.Logger }` + `func NewService(logger zerolog.Logger) *Service`
- `func (s *Service) CreateForOrder(ctx context.Context, q *sqlc.Queries, order sqlc.Order, r sqlc.Rfq) (sqlc.Invoice, error)`
  — inserts the invoice then one line item (via the passed tx-bound `q`); returns the invoice.
- `func (s *Service) GetForOrder(ctx context.Context, q *sqlc.Queries, orderID string) (sqlc.Invoice, []sqlc.InvoiceLineItem, error)`
- `func deriveLineItem(order sqlc.Order, r sqlc.Rfq) (description string, quantity, unitPriceCents, amountCents int64)`
  — description = RFQ title (fallback `"Order "+order.ID` if title empty); quantity = best-effort
  positive integer from `r.Requirements` JSON key `"quantity"`, else 1; `amountCents = order.TotalCents`
  (authoritative); `unitPriceCents = amountCents / quantity` (integer).

- [ ] **Step 1: Write failing helper tests** — `service_test.go`:
```go
func TestDeriveLineItemDefaultQuantity(t *testing.T) {
	o := sqlc.Order{ID: "o1", TotalCents: 5000}
	r := sqlc.Rfq{Title: "500 steel bolts", Requirements: []byte(`{}`)}
	desc, qty, unit, amt := deriveLineItem(o, r)
	require.Equal(t, "500 steel bolts", desc)
	require.Equal(t, int64(1), qty)
	require.Equal(t, int64(5000), unit)
	require.Equal(t, int64(5000), amt)
}

func TestDeriveLineItemParsesQuantity(t *testing.T) {
	o := sqlc.Order{ID: "o2", TotalCents: 6000}
	r := sqlc.Rfq{Title: "bolts", Requirements: []byte(`{"quantity": 3}`)}
	_, qty, unit, amt := deriveLineItem(o, r)
	require.Equal(t, int64(3), qty)
	require.Equal(t, int64(2000), unit)   // 6000/3
	require.Equal(t, int64(6000), amt)    // total stays exact
}

func TestDeriveLineItemEmptyTitleFallback(t *testing.T) {
	o := sqlc.Order{ID: "oX", TotalCents: 100}
	r := sqlc.Rfq{Title: "", Requirements: []byte(`{}`)}
	desc, _, _, _ := deriveLineItem(o, r)
	require.Equal(t, "Order oX", desc)
}
```
  (Confirm `sqlc.Rfq.Requirements`'s Go type — likely `[]byte`/`json.RawMessage`; adjust the literals to match. If it's a map type, build the map instead.)

- [ ] **Step 2: Run, verify fail** — `go test ./internal/invoicing/ -run Derive -v`.
- [ ] **Step 3: Implement** `deriveLineItem` (json.Unmarshal into `struct{ Quantity *json.Number }` or
  `map[string]any` and coerce; guard qty<=0 → 1) and `NewService`/`CreateForOrder`/`GetForOrder`.
- [ ] **Step 4: Run, verify pass.**
- [ ] **Step 5: Add a DB test** `TestCreateForOrder`: seed buyer/seller/rfq/quote/order (reuse the
  seed pattern from `internal/orders/orders_db_test.go` — copy a local `seedOrder` helper), call
  `CreateForOrder(ctx, q, order, rfq)` (pass the plain `q` — no tx needed for the test), assert:
  invoice row has `TotalCents==order.TotalCents`, `InvoiceNumber` matches `^INV-\d{4}-\d{6}$`
  (regexp), and `ListInvoiceLineItems` returns exactly one row with `AmountCents==order.TotalCents`.
- [ ] **Step 6: Run DB test, verify pass** (migration 020 applied).
- [ ] **Step 7: Commit** — `feat(invoicing): invoice service + line-item derivation`.

---

### Task 2: PDF renderer

**Files:** `internal/invoicing/renderer.go`, `internal/invoicing/renderer_test.go`.

**Interfaces (Produces):**
- `type Party struct{ Name string }`
- `func Render(inv sqlc.Invoice, lines []sqlc.InvoiceLineItem, seller, buyer Party) ([]byte, error)`

Renderer requirements (keep layout simple, A4 portrait, mm units):
- Header: "INVOICE" + `inv.InvoiceNumber` + issued date (`inv.IssuedAt.Format("2006-01-02")`).
- Two blocks: "From" (seller.Name) and "Bill to" (buyer.Name).
- A line-items table: Description | Qty | Unit | Amount, one row per line (money formatted from cents,
  e.g. `fmt.Sprintf("%s %.2f", inv.Currency, float64(cents)/100)`).
- Totals: Subtotal + Total (bold).
- Return `pdf.Output(&buf)` bytes; propagate `pdf.Error()`.

- [ ] **Step 1: Write failing test** — `renderer_test.go`:
```go
func TestRenderProducesPDF(t *testing.T) {
	inv := sqlc.Invoice{
		InvoiceNumber: "INV-2026-000123", Currency: "USD",
		SubtotalCents: 5000, TotalCents: 5000,
		IssuedAt: time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
	}
	lines := []sqlc.InvoiceLineItem{{Description: "500 steel bolts", Quantity: 1, UnitPriceCents: 5000, AmountCents: 5000}}
	b, err := Render(inv, lines, Party{Name: "Acme Steel"}, Party{Name: "BuildCo"})
	require.NoError(t, err)
	require.True(t, len(b) > 400, "pdf should be non-trivial")
	require.Equal(t, "%PDF-", string(b[:5]), "valid PDF header")
}
```
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement** `Render` using `github.com/go-pdf/fpdf`. Sketch:
```go
func Render(inv sqlc.Invoice, lines []sqlc.InvoiceLineItem, seller, buyer Party) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("Invoice "+inv.InvoiceNumber, false)
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 20)
	pdf.Cell(0, 12, "INVOICE")
	pdf.Ln(14)
	pdf.SetFont("Helvetica", "", 11)
	pdf.Cell(0, 6, inv.InvoiceNumber)
	pdf.Ln(6)
	pdf.Cell(0, 6, "Issued: "+inv.IssuedAt.Format("2006-01-02"))
	pdf.Ln(12)
	// From / Bill to
	pdf.SetFont("Helvetica", "B", 11); pdf.Cell(95, 6, "From"); pdf.Cell(95, 6, "Bill to"); pdf.Ln(6)
	pdf.SetFont("Helvetica", "", 11); pdf.Cell(95, 6, seller.Name); pdf.Cell(95, 6, buyer.Name); pdf.Ln(12)
	// Table header
	pdf.SetFont("Helvetica", "B", 10)
	pdf.Cell(100, 8, "Description"); pdf.Cell(20, 8, "Qty"); pdf.Cell(35, 8, "Unit"); pdf.Cell(35, 8, "Amount"); pdf.Ln(8)
	pdf.SetFont("Helvetica", "", 10)
	money := func(c int64) string { return fmt.Sprintf("%s %.2f", inv.Currency, float64(c)/100) }
	for _, li := range lines {
		pdf.Cell(100, 8, li.Description)
		pdf.Cell(20, 8, fmt.Sprintf("%d", li.Quantity))
		pdf.Cell(35, 8, money(li.UnitPriceCents))
		pdf.Cell(35, 8, money(li.AmountCents))
		pdf.Ln(8)
	}
	pdf.Ln(4)
	pdf.Cell(120, 8, ""); pdf.Cell(35, 8, "Subtotal"); pdf.Cell(35, 8, money(inv.SubtotalCents)); pdf.Ln(8)
	pdf.SetFont("Helvetica", "B", 11)
	pdf.Cell(120, 8, ""); pdf.Cell(35, 8, "Total"); pdf.Cell(35, 8, money(inv.TotalCents)); pdf.Ln(8)
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
```
- [ ] **Step 4: Run, verify pass** (`go mod tidy` first so fpdf resolves).
- [ ] **Step 5: Commit** — `feat(invoicing): pure-Go PDF invoice renderer`.

---

### Task 3: Generate invoice inside quote acceptance + notify buyer

**Files:** Modify `internal/quotes/service.go` and its tests.

- Add an `*invoicing.Service` field to `quotes.Service`; add it as the final param of `NewService`
  (existing: `NewService(q, pool, pub, loader, logger, notifier)` → append `inv *invoicing.Service`).
- In `Accept()`, AFTER `CreateOrder` and BEFORE `tx.Commit`, using the same `qtx` and the already-fetched
  RFQ `r`:
```go
	invoice, err := s.inv.CreateForOrder(ctx, qtx, order, r)
	if err != nil {
		return sqlc.Order{}, err
	}
```
- AFTER commit (with the other post-commit notifications), notify the buyer:
```go
	if s.notifier != nil {
		s.notifier.NotifyCompany(ctx, order.BuyerCompanyID, notify.Event{
			Type:  notify.TypeInvoiceIssued,
			Title: "Invoice issued",
			Body:  "Invoice " + invoice.InvoiceNumber + " is ready for your order.",
			Data:  map[string]any{"order_id": order.ID, "invoice_number": invoice.InvoiceNumber},
			CTALabel: "View invoice", CTAPath: "/orders/" + order.ID,
		})
	}
```
  (Keep the existing order.created notification too.)

- [ ] **Step 1: Update construction** in every `quotes.Service` test constructor and in
  `internal/httpapi/router.go` (build `invoicing.NewService(cfg.Logger)` once and pass it). Confirm
  `go build ./...` compiles.
- [ ] **Step 2: Extend an existing Accept test (or add one)** in `internal/quotes` to assert that after
  `Accept`, `GetInvoiceByOrder(ctx, order.ID)` returns an invoice with `TotalCents == quote.PriceCents`
  and exactly one line item. Run with TEST_DATABASE_URL + migrations applied → verify fail first, then pass.
- [ ] **Step 3: Implement** the Accept changes.
- [ ] **Step 4: Run** `go test ./internal/quotes/ ./internal/invoicing/ ./internal/notify/ -count=1 -v` → pass.
- [ ] **Step 5: Commit** — `feat(quotes): issue an invoice atomically on quote acceptance + notify buyer`.

---

### Task 4: PDF download endpoint + router wiring

**Files:** `internal/invoicing/handler.go`; wire in `internal/httpapi/router.go`.

**Handler:**
```go
type Handler struct{ svc *Service; q *sqlc.Queries }
func NewHandler(svc *Service, q *sqlc.Queries) *Handler { return &Handler{svc, q} }
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Group("/v1/orders", guard...).Get("/:id/invoice.pdf", h.pdf)
}
```
`pdf` handler:
1. `order, err := h.q.GetOrder(ctx, id)`; `pgx.ErrNoRows` → 404.
2. `me := middleware.CompanyID(c)`; if `me != order.BuyerCompanyID && me != order.SellerCompanyID` → 404.
3. `inv, lines, err := h.svc.GetForOrder(ctx, h.q, id)`; `pgx.ErrNoRows` → 404 "invoice not found".
4. Load party names: `h.q.GetCompanyByID` for buyer + seller → `Party{Name: co.Name}`.
5. `pdfBytes, err := Render(inv, lines, sellerParty, buyerParty)`.
6. `c.Set("Content-Type", "application/pdf")`; `c.Set("Content-Disposition", "inline; filename=\""+inv.InvoiceNumber+".pdf\"")`; `return c.Send(pdfBytes)`.

- [ ] **Step 1: Write failing handler test** — minimal Fiber app + fake guard injecting a company id
  (locals key from `pkg/middleware/tenant.go`), seed an order+invoice via a shared `q`, GET the route:
  assert 200 + `Content-Type: application/pdf` + body starts with `%PDF-` for a party; assert 404 for a
  non-party company id. (If the fake-guard wiring is awkward, assert the party/non-party logic at the
  service level and keep this a smoke test asserting 200 for a party.)
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement** the handler.
- [ ] **Step 4: Wire router** — after the quotes/orders registration, add:
```go
	invoicing.NewHandler(invSvc, q).Register(f, guard...)
```
  where `invSvc := invoicing.NewService(cfg.Logger)` (the same instance passed to quotes.NewService).
- [ ] **Step 5: Run** `go build ./... && go test ./internal/invoicing/ -count=1 -v` → pass.
- [ ] **Step 6: Commit** — `feat(invoicing): downloadable PDF invoice endpoint + router wiring`.

---

## Self-Review checklist

- `go build ./...` clean; `go mod tidy` leaves go.mod/go.sum consistent; `go-pdf/fpdf` is a DIRECT dep now.
- Full suite `go test ./... -p 1 -count=1` (TEST_DATABASE_URL set, migrations ≤020 applied) — 0 failures.
- Migration 020 round-trips (down/up) on the test DB.
- Accepting a quote creates exactly one invoice + one line item, atomic with the order (rollback leaves none).
- `invoice_number` matches `INV-YYYY-NNNNNN` and is UNIQUE (sequence-backed).
- PDF endpoint returns 404 for a non-party (no cross-tenant leak) and `application/pdf` for a party.
- Buyer receives an `invoice.issued` notification; existing `order.created` notification still fires.
