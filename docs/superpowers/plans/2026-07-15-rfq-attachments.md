# RFQ Attachments (Images + PDFs) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Buyers attach images/PDFs to RFQs (web chat + quote-thread replies); both the Core intake LLM and the seller agent see the real files; the agent's clarification loop is closed end-to-end via a new `quote.message` event.

**Architecture:** Core (Go/Fiber/sqlc/Postgres) gets an `attachments` table + disk blob store + upload/download endpoints; attachment IDs ride on chat messages and quote messages; the Anthropic calls (intake, extractor, agent quoting) send image/document content blocks; the gateway serves attachment bytes to agents; a new `quote.message` agent event delivers buyer replies to the seller agent, which re-quotes with the thread (capped at 2 clarification rounds).

**Tech Stack:** Go 1.x + Fiber v2 + sqlc + pgx/v5 + anthropic-sdk-go v1.56.0 + golang-migrate; Bun + TypeScript + TypeORM (SQLite, `synchronize:true`) + unpdf; Next.js 16 / React 19 (no Tailwind, hand-rolled globals.css).

**Spec:** `docs/superpowers/specs/2026-07-15-rfq-attachments-design.md`

## Global Constraints

- Allowed MIME types (server-side, sniffed from magic bytes, never the client header): `image/jpeg`, `image/png`, `image/webp`, `image/gif`, `application/pdf`.
- Max 10 MB per file (`10 << 20` bytes); max 5 attachments per message.
- Max 2 clarification rounds per RFQ on the agent side; after that the agent must quote or `no_bid`.
- Default attachments dir: `$ATTACHMENTS_DIR`, falling back to `~/tradebridge-data/attachments`.
- Models unchanged: Core intake/extraction default `claude-haiku-4-5`; agent reasoning default `claude-haiku-4-5`.
- No new dependencies in any repo (anthropic-sdk-go v1.56.0 already supports image/document blocks; agent already has `unpdf`).
- Event payload attachment metadata shape everywhere: `{"id": string, "filename": string, "mime": string, "size_bytes": number}`.
- Go repo root: `/home/deploy/b2b/tradebridge-core`. Agent repo root: `/home/deploy/b2b/tradebridge-agent`. Web app: `/home/deploy/b2b/tradebridge-core/web`.
- Go tests: `go test ./...`; DB-backed integration tests live in `test/integration` and skip unless `TEST_DATABASE_URL` is set. Agent tests: `bun test`. Web tests: `npm test` (vitest, lib/ only).
- Commit in the repo the task touches. Run `make generate` (sqlc) after changing `pkg/db/query/*.sql` or migrations.

---

### Task 1: Core — attachments migration + sqlc queries

**Files:**
- Create: `pkg/db/migrations/012_attachments.up.sql`
- Create: `pkg/db/migrations/012_attachments.down.sql`
- Create: `pkg/db/query/attachments.sql`
- Modify: `pkg/db/query/quotes.sql` (CreateQuoteMessage gains attachments column)
- Generated: `pkg/db/sqlc/*` via `make generate`

**Interfaces:**
- Produces (used by Tasks 2–8): `sqlc.Attachment{ID, CompanyID, RfqID *string, Filename, Mime string, SizeBytes int64, Sha256 string, CreatedAt time.Time}`, `q.CreateAttachment(ctx, CreateAttachmentParams{CompanyID, Filename, Mime, SizeBytes, Sha256}) (Attachment, error)`, `q.GetAttachment(ctx, id string) (Attachment, error)`, `q.SetAttachmentsRFQ(ctx, SetAttachmentsRFQParams{RfqID *string, Ids []string, CompanyID string}) error`, `q.SellerMatchedToRFQ(ctx, SellerMatchedToRFQParams{RfqID, SellerCompanyID string}) (bool, error)`.
- `sqlc.CreateQuoteMessageParams` gains `Attachments []byte` (nullable JSONB).

- [ ] **Step 1: Write the up migration**

`pkg/db/migrations/012_attachments.up.sql`:
```sql
-- Buyer-uploaded RFQ attachments (images + PDFs). Bytes live on local disk
-- (internal/attachments.Store); this table is metadata + authorization scope.
-- rfq_id is stamped once the attachment is referenced by an RFQ or a quote
-- message, and gates seller/agent download access.

CREATE TABLE attachments (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id TEXT NOT NULL REFERENCES companies(id),
    rfq_id     TEXT REFERENCES rfqs(id) ON DELETE SET NULL,
    filename   TEXT NOT NULL,
    mime       TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    sha256     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_attachments_company ON attachments (company_id, created_at DESC);
CREATE INDEX idx_attachments_rfq ON attachments (rfq_id);

-- Attachment metadata on buyer/seller thread messages:
-- [{"id","filename","mime","size_bytes"}]
ALTER TABLE quote_messages ADD COLUMN attachments JSONB;
```

`pkg/db/migrations/012_attachments.down.sql`:
```sql
ALTER TABLE quote_messages DROP COLUMN attachments;
DROP TABLE attachments;
```

- [ ] **Step 2: Write the queries**

`pkg/db/query/attachments.sql`:
```sql
-- name: CreateAttachment :one
INSERT INTO attachments (company_id, filename, mime, size_bytes, sha256)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAttachment :one
SELECT * FROM attachments WHERE id = $1;

-- name: SetAttachmentsRFQ :exec
UPDATE attachments SET rfq_id = @rfq_id
WHERE id = ANY(@ids::text[]) AND company_id = @company_id;

-- name: SellerMatchedToRFQ :one
SELECT EXISTS(
    SELECT 1 FROM rfq_matches
    WHERE rfq_id = $1 AND seller_company_id = $2
) AS matched;
```

In `pkg/db/query/quotes.sql`, replace the `CreateQuoteMessage` query with:
```sql
-- name: CreateQuoteMessage :one
INSERT INTO quote_messages (quote_id, author, body, attachments)
VALUES ($1, $2, $3, $4)
RETURNING *;
```

- [ ] **Step 3: Apply migration and regenerate**

Run: `cd /home/deploy/b2b/tradebridge-core && make migrate && make generate`
Expected: migration applies cleanly; `pkg/db/sqlc/attachments.sql.go` appears; `sqlc.Attachment` model exists in `pkg/db/sqlc/models.go`. (If the dev DB isn't running, `make migrate` may need the docker-compose Postgres up: `docker compose up -d postgres`.)

- [ ] **Step 4: Fix the one existing caller of CreateQuoteMessage**

`internal/gateway/commands.go` `threadClarification` (~line 137) now needs the new param field; pass `Attachments: nil`:
```go
	_, err := h.q.CreateQuoteMessage(ctx, sqlc.CreateQuoteMessageParams{
		QuoteID: quoteID, Author: "seller", Body: msg.Message, Attachments: nil,
	})
```
(Adapt to the actual local variable names in that function; only the `Attachments: nil` field is new.)

- [ ] **Step 5: Build + test**

Run: `go build ./... && go test ./...`
Expected: PASS (no behavior change yet).

- [ ] **Step 6: Commit**

```bash
git add pkg/db internal/gateway/commands.go
git commit -m "feat(db): attachments table + quote_messages.attachments column"
```

---

### Task 2: Core — `internal/attachments` package (store, sniffing, loader)

**Files:**
- Create: `internal/attachments/attachments.go`
- Create: `internal/attachments/attachments_test.go`

**Interfaces:**
- Consumes: Task 1 queries.
- Produces (used by Tasks 3–8):
  - `type Meta struct {ID string `json:"id"`; Filename string `json:"filename"`; Mime string `json:"mime"`; SizeBytes int64 `json:"size_bytes"`}`
  - `type Loaded struct {Meta; Data []byte}`
  - `func MetaOf(a sqlc.Attachment) Meta`
  - `const MaxFileBytes = 10 << 20`, `const MaxPerMessage = 5`
  - `func Sniff(data []byte) string`, `func Allowed(mime string) bool`
  - `func ResolveDir(configured string) (string, error)`
  - `type Store struct{...}`, `func NewStore(dir string) (*Store, error)`, methods `Put(id string, data []byte) error`, `Get(id string) ([]byte, error)`, `Delete(id string) error`
  - `type Loader struct{...}`, `func NewLoader(q *sqlc.Queries, store *Store) *Loader`, methods `LoadOwned(ctx, ids []string, companyID string) ([]Loaded, error)` (metadata + bytes; 400 on unknown id, 403 on foreign id) and `MetasOwned(ctx, ids []string, companyID string) ([]Meta, error)` (no disk read).

- [ ] **Step 1: Write failing tests**

`internal/attachments/attachments_test.go`:
```go
package attachments

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSniffAndAllowed(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 64)...)
	if m := Sniff(png); m != "image/png" {
		t.Fatalf("png sniff = %q", m)
	}
	pdf := append([]byte("%PDF-1.4\n"), make([]byte, 64)...)
	if m := Sniff(pdf); m != "application/pdf" {
		t.Fatalf("pdf sniff = %q", m)
	}
	if !Allowed("image/png") || !Allowed("application/pdf") {
		t.Fatal("png/pdf should be allowed")
	}
	if Allowed("text/html") || Allowed("application/zip") {
		t.Fatal("html/zip must not be allowed")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("att1", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("att1")
	if err != nil || !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("get = %q, %v", got, err)
	}
	if err := s.Delete("att1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("att1"); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestResolveDir(t *testing.T) {
	if d, err := ResolveDir("/x/y"); err != nil || d != "/x/y" {
		t.Fatalf("configured dir: %q, %v", d, err)
	}
	home, _ := os.UserHomeDir()
	d, err := ResolveDir("")
	if err != nil || d != filepath.Join(home, "tradebridge-data", "attachments") {
		t.Fatalf("default dir: %q, %v", d, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/attachments/`
Expected: FAIL — package does not exist / undefined symbols.

- [ ] **Step 3: Implement**

`internal/attachments/attachments.go`:
```go
// Package attachments stores buyer-uploaded RFQ files (images + PDFs): bytes
// on local disk behind Store (swap for S3 later without touching callers),
// metadata in Postgres, ownership-checked loading via Loader.
package attachments

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

const (
	MaxFileBytes  = 10 << 20 // 10 MB per file
	MaxPerMessage = 5
)

// Meta is the wire shape for attachment metadata in API responses, rfqs
// messages JSONB, quote_messages.attachments JSONB, and agent event payloads.
type Meta struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	Mime      string `json:"mime"`
	SizeBytes int64  `json:"size_bytes"`
}

// Loaded is Meta plus the file bytes, ready for an LLM call.
type Loaded struct {
	Meta
	Data []byte
}

func MetaOf(a sqlc.Attachment) Meta {
	return Meta{ID: a.ID, Filename: a.Filename, Mime: a.Mime, SizeBytes: a.SizeBytes}
}

var allowed = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
	"image/gif":       true,
	"application/pdf": true,
}

// Sniff detects the MIME type from magic bytes; the client header is never
// trusted.
func Sniff(data []byte) string {
	mt := http.DetectContentType(data)
	if i := strings.Index(mt, ";"); i >= 0 {
		mt = mt[:i]
	}
	return strings.TrimSpace(mt)
}

func Allowed(mime string) bool { return allowed[mime] }

// ResolveDir returns the configured dir, or ~/tradebridge-data/attachments.
func ResolveDir(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "tradebridge-data", "attachments"), nil
}

// Store is the disk blob store. IDs are DB-generated ULIDs — callers must
// pass IDs from an attachments row (never raw user input) so path traversal
// is impossible.
type Store struct{ dir string }

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(id string) string { return filepath.Join(s.dir, id) }

func (s *Store) Put(id string, data []byte) error { return os.WriteFile(s.path(id), data, 0o644) }
func (s *Store) Get(id string) ([]byte, error)    { return os.ReadFile(s.path(id)) }
func (s *Store) Delete(id string) error           { return os.Remove(s.path(id)) }

// Loader resolves attachment IDs to metadata (+bytes), enforcing that every
// ID was uploaded by companyID.
type Loader struct {
	q     *sqlc.Queries
	store *Store
}

func NewLoader(q *sqlc.Queries, store *Store) *Loader { return &Loader{q: q, store: store} }

func (l *Loader) rowOwned(ctx context.Context, id, companyID string) (sqlc.Attachment, error) {
	row, err := l.q.GetAttachment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, fiber.NewError(fiber.StatusBadRequest, "unknown attachment "+id)
	} else if err != nil {
		return row, err
	}
	if row.CompanyID != companyID {
		return row, fiber.NewError(fiber.StatusForbidden, "attachment does not belong to you")
	}
	return row, nil
}

func (l *Loader) MetasOwned(ctx context.Context, ids []string, companyID string) ([]Meta, error) {
	var out []Meta
	for _, id := range ids {
		row, err := l.rowOwned(ctx, id, companyID)
		if err != nil {
			return nil, err
		}
		out = append(out, MetaOf(row))
	}
	return out, nil
}

func (l *Loader) LoadOwned(ctx context.Context, ids []string, companyID string) ([]Loaded, error) {
	var out []Loaded
	for _, id := range ids {
		row, err := l.rowOwned(ctx, id, companyID)
		if err != nil {
			return nil, err
		}
		data, err := l.store.Get(row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, Loaded{Meta: MetaOf(row), Data: data})
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/attachments/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/attachments
git commit -m "feat(attachments): disk blob store, mime sniffing, ownership loader"
```

---

### Task 3: Core — upload + web download endpoints, config, router wiring

**Files:**
- Create: `internal/attachments/handler.go`
- Create: `internal/attachments/handler_test.go`
- Modify: `internal/config/config.go` (add `AttachmentsDir`)
- Modify: `internal/httpapi/router.go` (BodyLimit, construct store/loader/handler)
- Modify: `cmd/api/main.go` (pass `AttachmentsDir` through `httpapi.Config`)
- Create: `test/integration/api_attachments_test.go`

**Interfaces:**
- Consumes: Task 2 (`Store`, `Loader`, `Sniff`, `Allowed`, `MaxFileBytes`), Task 1 queries, `middleware.CompanyID`.
- Produces: `POST /v1/attachments` (multipart field `file`) → 201 `{id, filename, mime, size_bytes}`; `GET /v1/attachments/:id` → bytes (200) or 404. `attachments.NewHandler(q *sqlc.Queries, store *Store) *Handler` with `Register(app *fiber.App, guard ...fiber.Handler)`. `httpapi.Config` gains `AttachmentsDir string`; router exposes the built `*attachments.Store` and `*attachments.Loader` to later tasks (construct them once near the top of the handler-wiring block).

- [ ] **Step 1: Write failing handler tests (validation paths, no DB)**

`internal/attachments/handler_test.go`:
```go
package attachments

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func multipartBody(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	w.Close()
	return &buf, w.FormDataContentType()
}

func newTestApp(t *testing.T) *fiber.App {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{BodyLimit: 12 << 20})
	NewHandler(nil, store).Register(app) // nil queries: only pre-DB paths are exercised
	return app
}

func TestUploadRejectsMissingFile(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest("POST", "/v1/attachments", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestUploadRejectsDisallowedMime(t *testing.T) {
	app := newTestApp(t)
	body, ct := multipartBody(t, "file", "evil.html", []byte("<html><body>hi</body></html>"))
	req := httptest.NewRequest("POST", "/v1/attachments", body)
	req.Header.Set("Content-Type", ct)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
}

func TestUploadRejectsOversize(t *testing.T) {
	app := newTestApp(t)
	big := append([]byte("%PDF-1.4\n"), make([]byte, MaxFileBytes)...) // > 10 MB
	body, ct := multipartBody(t, "file", "big.pdf", big)
	req := httptest.NewRequest("POST", "/v1/attachments", body)
	req.Header.Set("Content-Type", ct)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/attachments/`
Expected: FAIL — `NewHandler` undefined.

- [ ] **Step 3: Implement the handler**

`internal/attachments/handler.go`:
```go
package attachments

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves buyer-facing attachment upload/download.
type Handler struct {
	q     *sqlc.Queries
	store *Store
}

func NewHandler(q *sqlc.Queries, store *Store) *Handler { return &Handler{q: q, store: store} }

func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/attachments", guard...)
	g.Post("/", h.upload)
	g.Get("/:id", h.download)
}

func (h *Handler) upload(c *fiber.Ctx) error {
	fh, err := c.FormFile("file")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "multipart field 'file' required")
	}
	if fh.Size > MaxFileBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "file exceeds 10 MB limit")
	}
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return err
	}
	if len(data) > MaxFileBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "file exceeds 10 MB limit")
	}
	mime := Sniff(data)
	if !Allowed(mime) {
		return fiber.NewError(fiber.StatusUnsupportedMediaType,
			"only JPEG, PNG, WebP, GIF images and PDFs are accepted")
	}
	sum := sha256.Sum256(data)
	row, err := h.q.CreateAttachment(c.Context(), sqlc.CreateAttachmentParams{
		CompanyID: middleware.CompanyID(c),
		Filename:  filepath.Base(fh.Filename),
		Mime:      mime,
		SizeBytes: int64(len(data)),
		Sha256:    hex.EncodeToString(sum[:]),
	})
	if err != nil {
		return err
	}
	if err := h.store.Put(row.ID, data); err != nil {
		// Compensate so no metadata row points at missing bytes.
		_ = h.q.DeleteAttachment(c.Context(), row.ID)
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(MetaOf(row))
}

// download authorizes: the uploader, the buyer of the referencing RFQ, or a
// matched seller of that RFQ. Everyone else sees 404 (no existence leak).
func (h *Handler) download(c *fiber.Ctx) error {
	row, err := h.q.GetAttachment(c.Context(), c.Params("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "attachment not found")
	} else if err != nil {
		return err
	}
	me := middleware.CompanyID(c)
	ok := row.CompanyID == me
	if !ok && row.RfqID != nil {
		if r, err := h.q.GetRFQ(c.Context(), *row.RfqID); err == nil && r.BuyerCompanyID == me {
			ok = true
		}
		if !ok {
			matched, err := h.q.SellerMatchedToRFQ(c.Context(), sqlc.SellerMatchedToRFQParams{
				RfqID: *row.RfqID, SellerCompanyID: me,
			})
			ok = err == nil && matched
		}
	}
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "attachment not found")
	}
	data, err := h.store.Get(row.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "attachment data missing")
	}
	c.Set(fiber.HeaderContentType, row.Mime)
	c.Set(fiber.HeaderContentDisposition, `inline; filename="`+row.Filename+`"`)
	return c.Send(data)
}
```

Add the `DeleteAttachment` query to `pkg/db/query/attachments.sql` and regenerate:
```sql
-- name: DeleteAttachment :exec
DELETE FROM attachments WHERE id = $1;
```
Run: `make generate`

- [ ] **Step 4: Wire config + router**

`internal/config/config.go` — add to the struct (after `AnthropicModel`):
```go
	// AttachmentsDir is where RFQ attachment bytes are stored on disk.
	// Empty means ~/tradebridge-data/attachments.
	AttachmentsDir string `mapstructure:"ATTACHMENTS_DIR"`
```
and in `Load()`:
```go
	v.SetDefault("ATTACHMENTS_DIR", "")
```

`internal/httpapi/router.go`:
1. Add `AttachmentsDir string` to the `Config` struct.
2. In the `fiber.New(fiber.Config{...})` call, add `BodyLimit: 12 * 1024 * 1024` (Fiber's default 4 MB would reject 10 MB uploads).
3. In the handler-wiring block (near `pub := eventbus.New(...)`, before the rfq/quotes registrations), construct and register:
```go
	attDir, err := attachments.ResolveDir(cfg.AttachmentsDir)
	if err != nil {
		panic(err) // match how the router treats unrecoverable setup errors; adapt if it returns errors instead
	}
	attStore, err := attachments.NewStore(attDir)
	if err != nil {
		panic(err)
	}
	attLoader := attachments.NewLoader(q, attStore)
	attachments.NewHandler(q, attStore).Register(f, guard...)
	_ = attLoader // consumed by rfq/quotes services in Tasks 6–7
```
(If `httpapi.New` returns `(app, error)`, return the error instead of panicking — follow the existing style in that function.)

`cmd/api/main.go` — add `AttachmentsDir: cfg.AttachmentsDir,` to the `httpapi.Config{...}` literal.

- [ ] **Step 5: Run unit tests**

Run: `go build ./... && go test ./internal/attachments/`
Expected: PASS.

- [ ] **Step 6: Write the integration test (upload → download happy path)**

`test/integration/api_attachments_test.go` (follow `api_a1_test.go` helpers `buildAPI`, `doJSON`, `uniqueEmail`; those set up a registered company + token):
```go
package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// uploadPNG posts a tiny valid PNG as multipart and returns the attachment id.
func uploadPNG(t *testing.T, app appUnderTest, token string) map[string]any {
	t.Helper()
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 64)...)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "spec.png")
	require.NoError(t, err)
	_, err = fw.Write(png)
	require.NoError(t, err)
	w.Close()

	req := httptest.NewRequest("POST", "/v1/attachments", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out
}

func TestAttachmentUploadDownload(t *testing.T) {
	app, token := buildAPIWithUser(t) // adapt to this suite's existing register+login helper
	meta := uploadPNG(t, app, token)
	require.Equal(t, "image/png", meta["mime"])
	id := meta["id"].(string)

	req := httptest.NewRequest("GET", "/v1/attachments/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "image/png", resp.Header.Get("Content-Type"))
	body, _ := io.ReadAll(resp.Body)
	require.Len(t, body, 72)

	// A different company must get 404.
	_, token2 := buildAPIUserOnApp(t, app) // second registered company on the same app
	req2 := httptest.NewRequest("GET", "/v1/attachments/"+id, nil)
	req2.Header.Set("Authorization", "Bearer "+token2)
	resp2, err := app.Test(req2, -1)
	require.NoError(t, err)
	require.Equal(t, 404, resp2.StatusCode)
}
```
Note: `buildAPIWithUser` / `buildAPIUserOnApp` are stand-ins — reuse whatever `test/integration/api_a1_test.go` actually provides to register a company and obtain a token (`buildAPI` + register/login via `doJSON`); also ensure the test uses a temp `AttachmentsDir` (`httpapi.Config{AttachmentsDir: t.TempDir(), ...}` in the builder). Keep the test's substance (201 + mime + bytes round-trip + cross-tenant 404) exactly as written.

- [ ] **Step 7: Run integration tests (if DB available)**

Run: `TEST_DATABASE_URL=<dev-db-url> go test ./test/integration/ -run TestAttachmentUploadDownload -v`
Expected: PASS (or SKIP if no DB configured — then verify via unit tests + `go build`).

- [ ] **Step 8: Commit**

```bash
git add internal/attachments internal/config internal/httpapi cmd/api pkg/db test/integration
git commit -m "feat(attachments): upload + party-scoped download endpoints"
```

---

### Task 4: Core — multimodal intake (ChatMessage attachments → Anthropic blocks)

**Files:**
- Modify: `internal/rfq/requirements.go` (extend `ChatMessage`)
- Create: `internal/rfq/multimodal.go`
- Create: `internal/rfq/multimodal_test.go`
- Modify: `internal/rfq/intake_llm.go` (anthropic + openai adapters)

**Interfaces:**
- Consumes: `attachments.Loaded`, `attachments.Meta` (Task 2).
- Produces:
  - `ChatMessage` gains `AttachmentIDs []string `json:"attachment_ids,omitempty"``, `Attachments []attachments.Meta `json:"attachments,omitempty"``, `Loaded []attachments.Loaded `json:"-"``.
  - `func userContent(text string, atts []attachments.Loaded) []anthropic.ContentBlockParamUnion` — shared by intake (this task) and extractor (Task 5).
  - `func attachmentNote(text string, atts []attachments.Loaded) string` — text-only fallback marker for OpenAI/heuristic paths.

- [ ] **Step 1: Extend ChatMessage**

In `internal/rfq/requirements.go` replace the `ChatMessage` type with:
```go
// ChatMessage is one turn of the buyer intake conversation. AttachmentIDs
// come from the client; Attachments (metadata) are hydrated server-side and
// persisted in rfqs.messages; Loaded (bytes) exists only in memory for the
// LLM call and is never serialized.
type ChatMessage struct {
	Role          string               `json:"role"` // "user" | "assistant"
	Content       string               `json:"content"`
	AttachmentIDs []string             `json:"attachment_ids,omitempty"`
	Attachments   []attachments.Meta   `json:"attachments,omitempty"`
	Loaded        []attachments.Loaded `json:"-"`
}
```
(Add the `attachments` import: `"github.com/tradebridge/core/internal/attachments"`.)

- [ ] **Step 2: Write failing tests for the block builder**

`internal/rfq/multimodal_test.go`:
```go
package rfq

import (
	"testing"

	"github.com/tradebridge/core/internal/attachments"
)

func loadedPNG() attachments.Loaded {
	return attachments.Loaded{
		Meta: attachments.Meta{ID: "a1", Filename: "photo.png", Mime: "image/png", SizeBytes: 3},
		Data: []byte{1, 2, 3},
	}
}

func loadedPDF() attachments.Loaded {
	return attachments.Loaded{
		Meta: attachments.Meta{ID: "a2", Filename: "spec.pdf", Mime: "application/pdf", SizeBytes: 3},
		Data: []byte("%PD"),
	}
}

func TestUserContentBlocks(t *testing.T) {
	blocks := userContent("need 500 units", []attachments.Loaded{loadedPNG(), loadedPDF()})
	if len(blocks) != 3 {
		t.Fatalf("len(blocks) = %d, want 3 (image, document, text)", len(blocks))
	}
	if blocks[0].OfImage == nil {
		t.Fatal("block 0 should be an image block")
	}
	if blocks[1].OfDocument == nil {
		t.Fatal("block 1 should be a document block")
	}
	if blocks[2].OfText == nil || blocks[2].OfText.Text != "need 500 units" {
		t.Fatal("block 2 should be the text block")
	}
}

func TestUserContentTextOnly(t *testing.T) {
	blocks := userContent("hello", nil)
	if len(blocks) != 1 || blocks[0].OfText == nil {
		t.Fatalf("want single text block, got %+v", blocks)
	}
}

func TestAttachmentNote(t *testing.T) {
	got := attachmentNote("hello", []attachments.Loaded{loadedPDF()})
	want := "hello\n[attached: spec.pdf]"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if attachmentNote("hello", nil) != "hello" {
		t.Fatal("no attachments must leave text unchanged")
	}
}

func TestAnthropicParamsCarryAttachments(t *testing.T) {
	a := anthropicChat{model: "claude-haiku-4-5"}
	msgs := []ChatMessage{{Role: "user", Content: "hi", Loaded: []attachments.Loaded{loadedPNG()}}}
	params := a.params("sys", msgs)
	if len(params.Messages) != 1 {
		t.Fatalf("messages = %d", len(params.Messages))
	}
	if len(params.Messages[0].Content) != 2 {
		t.Fatalf("content blocks = %d, want 2 (image + text)", len(params.Messages[0].Content))
	}
}
```
(If the SDK union field names differ from `OfImage`/`OfDocument`/`OfText`, check `pkg/mod/.../anthropic-sdk-go@v1.56.0/message.go` for the exact `ContentBlockParamUnion` field names and adjust the assertions — the constructors `NewImageBlockBase64`, `NewDocumentBlock`, `NewTextBlock` are the stable API.)

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/rfq/ -run 'TestUserContent|TestAttachmentNote|TestAnthropicParams'`
Expected: FAIL — `userContent`/`attachmentNote` undefined.

- [ ] **Step 4: Implement the builders and adapt the adapters**

`internal/rfq/multimodal.go`:
```go
package rfq

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/tradebridge/core/internal/attachments"
)

// userContent builds the Anthropic content blocks for one user turn:
// attachments first (images as vision blocks, PDFs as document blocks), then
// the text.
func userContent(text string, atts []attachments.Loaded) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion
	for _, a := range atts {
		b64 := base64.StdEncoding.EncodeToString(a.Data)
		if a.Mime == "application/pdf" {
			blocks = append(blocks, anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{Data: b64}))
		} else {
			blocks = append(blocks, anthropic.NewImageBlockBase64(a.Mime, b64))
		}
	}
	return append(blocks, anthropic.NewTextBlock(text))
}

// attachmentNote is the text-only degradation for providers without
// vision/document support: append one marker line per attachment.
func attachmentNote(text string, atts []attachments.Loaded) string {
	if len(atts) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(text)
	for _, a := range atts {
		fmt.Fprintf(&b, "\n[attached: %s]", a.Filename)
	}
	return b.String()
}
```

In `internal/rfq/intake_llm.go`, `anthropicChat.params` — replace the user branch of the message loop:
```go
	for _, m := range msgs {
		if m.Role == "assistant" {
			params.Messages = append(params.Messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		} else {
			params.Messages = append(params.Messages, anthropic.NewUserMessage(userContent(m.Content, m.Loaded)...))
		}
	}
```

In the OpenAI-compatible adapter in the same file (`openAIChat` or equivalent — wherever it maps `ChatMessage.Content` into its request), wrap user content with the fallback marker:
```go
		content := m.Content
		if m.Role != "assistant" {
			content = attachmentNote(m.Content, m.Loaded)
		}
```
(The heuristic intake needs no change — it only reads `Content`.)

- [ ] **Step 5: Run tests**

Run: `go test ./internal/rfq/`
Expected: PASS (all existing intake tests still green).

- [ ] **Step 6: Commit**

```bash
git add internal/rfq
git commit -m "feat(rfq): multimodal intake — image/PDF content blocks on chat messages"
```

---

### Task 5: Core — multimodal requirements extractor

**Files:**
- Modify: `internal/rfq/extractor.go` (interface signature)
- Modify: `internal/rfq/claude.go` (blocks)
- Modify: `internal/rfq/openai.go` (fallback marker)
- Modify: `internal/rfq/requirements.go` or wherever `HeuristicExtractor` lives (signature only)
- Modify: `internal/rfq/service.go` + any test fakes (call sites)

**Interfaces:**
- Consumes: `userContent`, `attachmentNote` (Task 4).
- Produces: `Extractor` interface becomes
  `Extract(ctx context.Context, title, text string, atts []attachments.Loaded) (Requirements, error)` — Task 6 calls it with the RFQ's loaded attachments.

- [ ] **Step 1: Change the interface**

`internal/rfq/extractor.go`:
```go
type Extractor interface {
	Extract(ctx context.Context, title, text string, atts []attachments.Loaded) (Requirements, error)
}
```

- [ ] **Step 2: Update every implementation**

`internal/rfq/claude.go` — signature + multimodal user message:
```go
func (e *claudeExtractor) Extract(ctx context.Context, title, text string, atts []attachments.Loaded) (Requirements, error) {
	msg, err := e.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     e.model,
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: e.system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(userContent(
				fmt.Sprintf("Title: %s\n\nRFQ:\n%s", title, text), atts)...),
		},
	})
	// ... rest unchanged
```

`internal/rfq/openai.go` — signature + text fallback: change the method signature the same way and pass `attachmentNote(<existing prompt string>, atts)` where the user content string is built.

`HeuristicExtractor` — signature only; ignore `atts`:
```go
func (HeuristicExtractor) Extract(ctx context.Context, title, text string, _ []attachments.Loaded) (Requirements, error) {
```

- [ ] **Step 3: Fix all call sites**

`grep -rn "\.Extract(" internal/ test/` and add the attachments argument. In `internal/rfq/service.go` `CreateAndMatch`, pass the new `atts` parameter (added in Task 6 — for now pass `nil` to keep this task compiling: `ex.Extract(ctx, title, text, nil)`). Update test fakes implementing `Extractor` (e.g. in `rfq_test.go`) to the new signature.

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/rfq/`
Expected: PASS. `TestHeuristicExtract` updated to call `Extract(ctx, title, text, nil)`.

- [ ] **Step 5: Commit**

```bash
git add internal/rfq test
git commit -m "feat(rfq): extractor accepts attachments for multimodal extraction"
```

---

### Task 6: Core — rfq service hydrates attachments, stamps rfq_id, event carries metadata

**Files:**
- Modify: `internal/rfq/service.go`
- Modify: `internal/rfq/handler.go` (per-message cap check)
- Modify: `internal/httpapi/router.go` (pass loader to `rfq.NewService`)
- Modify: `internal/rfq/handler_stream_test.go` + other `NewService` call sites
- Create/extend: `test/integration/api_chat_attachments_test.go`

**Interfaces:**
- Consumes: `attachments.Loader` (Task 2), extended `ChatMessage` (Task 4), extractor (Task 5), Task 1 queries.
- Produces:
  - `rfq.NewService(q, pub, resolver, loader *attachments.Loader, logger, ttlDays)` (loader may be nil in unit tests — hydration is skipped when nil or when no message has attachment IDs).
  - `CreateAndMatch(ctx, companyID, userID, title, text string, atts []attachments.Loaded)` — extra final param.
  - `matchedEvent` gains `Attachments []attachments.Meta `json:"attachments,omitempty"`` — the agent (Task 12) reads `payload.attachments`.

- [ ] **Step 1: Extend the service**

`internal/rfq/service.go`:

1. Add `loader *attachments.Loader` field to `Service`; extend `NewService(q, pub, resolver, loader, logger, ttlDays)`.
2. Add hydration (called at the top of both `Chat` and `ChatStream`, before intake):
```go
// hydrateAttachments resolves each message's AttachmentIDs into metadata +
// bytes, enforcing ownership and the per-message cap. No-op without a loader
// (unit tests) or without attachment IDs.
func (s *Service) hydrateAttachments(ctx context.Context, companyID string, msgs []ChatMessage) error {
	if s.loader == nil {
		return nil
	}
	for i := range msgs {
		if len(msgs[i].AttachmentIDs) == 0 {
			continue
		}
		if len(msgs[i].AttachmentIDs) > attachments.MaxPerMessage {
			return fiber.NewError(fiber.StatusBadRequest, "too many attachments (max 5 per message)")
		}
		loaded, err := s.loader.LoadOwned(ctx, msgs[i].AttachmentIDs, companyID)
		if err != nil {
			return err
		}
		msgs[i].Loaded = loaded
		msgs[i].Attachments = make([]attachments.Meta, len(loaded))
		for j, l := range loaded {
			msgs[i].Attachments[j] = l.Meta
		}
	}
	return nil
}
```
3. In `Chat` and `ChatStream`, call it first:
```go
	if err := s.hydrateAttachments(ctx, buyerCompanyID, msgs); err != nil {
		return ChatResult{}, err
	}
```
4. `finishChat`: collect all loaded attachments from the transcript and pass them to `CreateAndMatch`:
```go
	var atts []attachments.Loaded
	for _, m := range msgs {
		atts = append(atts, m.Loaded...)
	}
	opened, matches, err := s.CreateAndMatch(ctx, buyerCompanyID, userID, dec.Title, dec.Text, atts)
```
5. `CreateAndMatch(ctx, companyID, userID, title, text string, atts []attachments.Loaded)`:
   - pass `atts` to `ex.Extract(ctx, title, text, atts)` (replacing the Task 5 `nil`),
   - after `CreateRFQ` succeeds, stamp ownership scope:
```go
	if len(atts) > 0 {
		ids := make([]string, len(atts))
		for i, a := range atts {
			ids[i] = a.ID
		}
		if err := s.q.SetAttachmentsRFQ(ctx, sqlc.SetAttachmentsRFQParams{
			RfqID: &rfq.ID, Ids: ids, CompanyID: companyID,
		}); err != nil {
			s.logger.Warn().Err(err).Str("rfq_id", rfq.ID).Msg("failed to stamp attachments")
		}
	}
```
   - collect metas and thread them into `broadcast` so every match's `rfq.matched` payload includes them:
```go
type matchedEvent struct {
	Type           string             `json:"type"`
	RFQID          string             `json:"rfq_id"`
	Title          string             `json:"title"`
	BuyerCompanyID string             `json:"buyer_company_id"`
	Requirements   Requirements       `json:"requirements"`
	Score          float64            `json:"score"`
	Reason         string             `json:"reason"`
	Attachments    []attachments.Meta `json:"attachments,omitempty"`
}
```
   (add an `atts []attachments.Meta` parameter to `broadcast` and set `Attachments: atts`).
6. The plain `POST /v1/rfqs` path (`handler.go create`) passes `nil` attachments.

- [ ] **Step 2: Update call sites**

- `internal/httpapi/router.go`: `rfq.NewService(q, pub, settingsSvc, attLoader, cfg.Logger, cfg.RFQTTLDays)` (remove the `_ = attLoader` placeholder from Task 3).
- `internal/rfq/handler_stream_test.go` and any other `NewService(...)` calls: add `nil` for the loader.

- [ ] **Step 3: Run unit tests**

Run: `go build ./... && go test ./internal/rfq/ ./internal/httpapi/...`
Expected: PASS.

- [ ] **Step 4: Integration test — chat with attachment persists metadata + stamps rfq_id + event payload**

`test/integration/api_chat_attachments_test.go`: reuse the Task 3 upload helper; then drive `/v1/rfq-chat` with enough detail that the heuristic intake creates an RFQ (mirror `api_chat_test.go`), including `"attachment_ids": ["<id>"]` on the first user message. Assert:
1. response `done == true` and the returned `rfq.messages[0].attachments[0].filename == "spec.png"`,
2. `SELECT rfq_id FROM attachments WHERE id = $1` equals the created RFQ id (direct pool query),
3. `SELECT payload FROM agent_events WHERE type = 'rfq.matched' AND payload->>'rfq_id' = $1` — if a seller matched, payload contains `attachments` with the id (skip this assert when `match_count == 0`; seed a matching seller company the same way `api_chat_test.go` or the quotes integration tests do).

Run: `TEST_DATABASE_URL=<dev-db-url> go test ./test/integration/ -run TestChatAttachments -v`
Expected: PASS (or SKIP without DB).

- [ ] **Step 5: Commit**

```bash
git add internal test
git commit -m "feat(rfq): hydrate chat attachments, stamp rfq_id, attach metadata to rfq.matched"
```

---

### Task 7: Core — quote-thread attachments + `quote.message` event

**Files:**
- Modify: `internal/quotes/service.go` (new `AddMessage` + event)
- Modify: `internal/quotes/handler.go` (`messageReq`, delegate to service)
- Modify: `internal/httpapi/router.go` (`quotes.NewService` gains loader + logger)
- Create: `internal/quotes/message_test.go`
- Modify/extend: `test/integration` quote-message test

**Interfaces:**
- Consumes: `attachments.Loader`, `attachments.Meta`, `attachments.MaxPerMessage` (Task 2), `eventbus.Publisher.Publish` (exists).
- Produces:
  - `quotes.NewService(q, pool, pub, loader *attachments.Loader, logger zerolog.Logger)`.
  - `func (s *Service) AddMessage(ctx, quote sqlc.Quote, authorCompanyID, body string, attachmentIDs []string) (sqlc.QuoteMessage, error)`.
  - Event `quote.message` to the seller company, payload:
    `{"type":"quote.message","rfq_id":...,"quote_id":...,"author":"buyer","body":...,"attachments":[Meta...]}` — consumed by agent Task 13.

- [ ] **Step 1: Write the failing service test**

`internal/quotes/message_test.go` — pure marshaling test for the event payload shape (network/DB paths are covered by the integration test):
```go
package quotes

import (
	"encoding/json"
	"testing"

	"github.com/tradebridge/core/internal/attachments"
)

func TestQuoteMessageEventShape(t *testing.T) {
	ev := QuoteMessageEvent{
		Type: "quote.message", RFQID: "r1", QuoteID: "q1", Author: "buyer", Body: "here is the photo",
		Attachments: []attachments.Meta{{ID: "a1", Filename: "x.png", Mime: "image/png", SizeBytes: 5}},
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"type", "rfq_id", "quote_id", "author", "body", "attachments"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing key %q in %s", k, b)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/quotes/ -run TestQuoteMessageEventShape`
Expected: FAIL — `QuoteMessageEvent` undefined.

- [ ] **Step 3: Implement service AddMessage + event**

`internal/quotes/service.go`:
```go
// QuoteMessageEvent is the agent event emitted when a buyer posts in a quote
// thread, closing the clarification loop (the agent asked via
// clarification.request; this is how it hears the answer).
type QuoteMessageEvent struct {
	Type        string             `json:"type"`
	RFQID       string             `json:"rfq_id"`
	QuoteID     string             `json:"quote_id"`
	Author      string             `json:"author"`
	Body        string             `json:"body"`
	Attachments []attachments.Meta `json:"attachments,omitempty"`
}
```
Extend the struct/constructor:
```go
type Service struct {
	q      *sqlc.Queries
	pool   *pgxpool.Pool
	pub    *eventbus.Publisher
	loader *attachments.Loader
	logger zerolog.Logger
}

func NewService(q *sqlc.Queries, pool *pgxpool.Pool, pub *eventbus.Publisher, loader *attachments.Loader, logger zerolog.Logger) *Service {
	return &Service{q: q, pool: pool, pub: pub, loader: loader, logger: logger}
}
```
Add:
```go
// AddMessage appends a thread message (with optional attachments) and, when
// the author is the buyer, emits a quote.message event to the seller's agent.
// Event emission is best-effort: the message row is the source of truth.
func (s *Service) AddMessage(ctx context.Context, q sqlc.Quote, authorCompanyID, body string, attachmentIDs []string) (sqlc.QuoteMessage, error) {
	author := "buyer"
	if q.SellerCompanyID == authorCompanyID {
		author = "seller"
	}
	var metas []attachments.Meta
	if len(attachmentIDs) > 0 {
		if s.loader == nil {
			return sqlc.QuoteMessage{}, fiber.NewError(fiber.StatusBadRequest, "attachments not supported")
		}
		var err error
		metas, err = s.loader.MetasOwned(ctx, attachmentIDs, authorCompanyID)
		if err != nil {
			return sqlc.QuoteMessage{}, err
		}
	}
	var attsJSON []byte
	if len(metas) > 0 {
		attsJSON, _ = json.Marshal(metas)
	}
	msg, err := s.q.CreateQuoteMessage(ctx, sqlc.CreateQuoteMessageParams{
		QuoteID: q.ID, Author: author, Body: body, Attachments: attsJSON,
	})
	if err != nil {
		return msg, err
	}
	if len(attachmentIDs) > 0 {
		if err := s.q.SetAttachmentsRFQ(ctx, sqlc.SetAttachmentsRFQParams{
			RfqID: &q.RfqID, Ids: attachmentIDs, CompanyID: authorCompanyID,
		}); err != nil {
			s.logger.Warn().Err(err).Str("quote_id", q.ID).Msg("failed to stamp attachments")
		}
	}
	if author == "buyer" {
		ev := QuoteMessageEvent{
			Type: "quote.message", RFQID: q.RfqID, QuoteID: q.ID,
			Author: author, Body: body, Attachments: metas,
		}
		b, _ := json.Marshal(ev)
		if err := s.pub.Publish(ctx, q.SellerCompanyID, "quote.message", b); err != nil {
			s.logger.Warn().Err(err).Str("quote_id", q.ID).Msg("quote.message publish failed")
		}
	}
	return msg, nil
}
```

- [ ] **Step 4: Delegate from the handler**

`internal/quotes/handler.go` — replace `messageReq` and `addMessage`:
```go
type messageReq struct {
	Body          string   `json:"body"`
	AttachmentIDs []string `json:"attachment_ids"`
}

func (h *Handler) addMessage(c *fiber.Ctx) error {
	q, err := h.mustParty(c)
	if err != nil {
		return err
	}
	var req messageReq
	if err := c.BodyParser(&req); err != nil || (req.Body == "" && len(req.AttachmentIDs) == 0) {
		return fiber.NewError(fiber.StatusBadRequest, "body or attachments required")
	}
	if len(req.AttachmentIDs) > attachments.MaxPerMessage {
		return fiber.NewError(fiber.StatusBadRequest, "too many attachments (max 5 per message)")
	}
	msg, err := h.svc.AddMessage(c.Context(), q, middleware.CompanyID(c), req.Body, req.AttachmentIDs)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(msg)
}
```

- [ ] **Step 4b: JSON view for message rows (avoid base64-encoded JSONB)**

sqlc generates `QuoteMessage.Attachments []byte`, and `encoding/json` marshals `[]byte` as a **base64 string** — so returning sqlc rows directly would break the web client, which expects a JSON array. Add a view in `internal/quotes/handler.go` and use it in both `addMessage` (the 201 response) and `listMessages`:
```go
type quoteMessageView struct {
	ID          string          `json:"id"`
	QuoteID     string          `json:"quote_id"`
	Author      string          `json:"author"`
	Body        string          `json:"body"`
	Attachments json.RawMessage `json:"attachments"`
	CreatedAt   time.Time       `json:"created_at"`
}

func messageView(m sqlc.QuoteMessage) quoteMessageView {
	v := quoteMessageView{
		ID: m.ID, QuoteID: m.QuoteID, Author: m.Author, Body: m.Body, CreatedAt: m.CreatedAt,
	}
	if len(m.Attachments) > 0 {
		v.Attachments = json.RawMessage(m.Attachments)
	}
	return v
}
```
- `addMessage`: `return c.Status(fiber.StatusCreated).JSON(messageView(msg))`
- `listMessages`: map the slice before returning:
```go
	views := make([]quoteMessageView, len(msgs))
	for i, m := range msgs {
		views[i] = messageView(m)
	}
	return c.JSON(fiber.Map{"messages": views, "count": len(views)})
```
(Adjust the `CreatedAt` field type to whatever `sqlc.QuoteMessage.CreatedAt` actually is — `time.Time` per the sqlc override.)

- [ ] **Step 5: Update call sites + run tests**

`internal/httpapi/router.go`: `quotes.NewHandler(quotes.NewService(q, cfg.Pool, pub, attLoader, cfg.Logger), q).Register(f, guard...)`. Fix any other `quotes.NewService` call (grep).

Run: `go build ./... && go test ./internal/quotes/`
Expected: PASS.

- [ ] **Step 6: Integration test**

Extend the quotes integration flow (find the existing quote-message or accept-flow test in `test/integration/`): buyer posts `POST /v1/quotes/:id/messages` with `{"body":"see photo","attachment_ids":["<uploaded id>"]}` →
1. 201 and response contains the attachments metadata,
2. a row in `agent_events` with `type='quote.message'` for the seller company whose payload has `body`, `quote_id`, and `attachments[0].id`,
3. seller-authored post produces NO `quote.message` event.

Run: `TEST_DATABASE_URL=<dev-db-url> go test ./test/integration/ -run QuoteMessage -v`
Expected: PASS (or SKIP without DB).

- [ ] **Step 7: Commit**

```bash
git add internal test
git commit -m "feat(quotes): thread attachments + quote.message agent event"
```

---

### Task 8: Core — gateway attachment download for agents

**Files:**
- Modify: `internal/gateway/gateway.go` (handler field + route)
- Create: `internal/gateway/attachment.go`
- Modify: `cmd/gateway/main.go` (build store)
- Modify: any `gateway.NewHandler` call sites (grep for tests)

**Interfaces:**
- Consumes: `attachments.Store`, `ResolveDir` (Task 2), `SellerMatchedToRFQ` (Task 1), existing `authenticate`.
- Produces: `GET /agent/v1/attachments/:id` — Bearer agent credential; 200 bytes with `Content-Type` when the agent's company is matched to the attachment's RFQ; 404 otherwise. `gateway.NewHandler(q, rdb, store *attachments.Store, logger)`.

- [ ] **Step 1: Extend the handler struct + registration**

`internal/gateway/gateway.go`:
```go
type Handler struct {
	q      *sqlc.Queries
	redis  *redis.Client
	store  *attachments.Store
	logger zerolog.Logger
}

func NewHandler(q *sqlc.Queries, rdb *redis.Client, store *attachments.Store, logger zerolog.Logger) *Handler {
	return &Handler{q: q, redis: rdb, store: store, logger: logger}
}

func (h *Handler) Register(app *fiber.App) {
	app.Get("/agent/v1/stream", h.stream)
	app.Post("/agent/v1/commands", h.command)
	app.Post("/agent/v1/events/:seq/ack", h.ack)
	app.Post("/agent/v1/heartbeat", h.heartbeat)
	app.Get("/agent/v1/attachments/:id", h.attachment)
}
```

- [ ] **Step 2: Implement the route**

`internal/gateway/attachment.go`:
```go
package gateway

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// attachment serves RFQ attachment bytes to an authenticated agent whose
// company is matched to the attachment's RFQ. Anything else is a 404 so
// attachment IDs don't leak existence.
func (h *Handler) attachment(c *fiber.Ctx) error {
	agent, err := h.authenticate(c)
	if err != nil {
		return err
	}
	row, err := h.q.GetAttachment(c.Context(), c.Params("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "attachment not found")
	} else if err != nil {
		return err
	}
	allowed := false
	if row.RfqID != nil {
		matched, err := h.q.SellerMatchedToRFQ(c.Context(), sqlc.SellerMatchedToRFQParams{
			RfqID: *row.RfqID, SellerCompanyID: agent.CompanyID,
		})
		allowed = err == nil && matched
	}
	if !allowed {
		return fiber.NewError(fiber.StatusNotFound, "attachment not found")
	}
	data, err := h.store.Get(row.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "attachment data missing")
	}
	c.Set(fiber.HeaderContentType, row.Mime)
	c.Set(fiber.HeaderContentDisposition, `inline; filename="`+row.Filename+`"`)
	return c.Send(data)
}
```

- [ ] **Step 3: Wire the store in the gateway binary**

`cmd/gateway/main.go`:
```go
	attDir, err := attachments.ResolveDir(app.Config.AttachmentsDir)
	if err != nil {
		panic(err)
	}
	store, err := attachments.NewStore(attDir)
	if err != nil {
		panic(err)
	}

	gateway.NewHandler(app.Queries, app.Redis, store, app.Logger).Register(f)
```
(imports: `"github.com/tradebridge/core/internal/attachments"`.)

- [ ] **Step 4: Fix other NewHandler call sites**

Run: `grep -rn "gateway.NewHandler" --include="*.go" .` and add a store argument (tests can use `attachments.NewStore(t.TempDir())` or `nil` if the test never hits the attachment route).

- [ ] **Step 5: Build + test**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/gateway cmd/gateway
git commit -m "feat(gateway): serve RFQ attachments to matched agents"
```

---

### Task 9: Agent — fetch attachments from the gateway (with disk cache)

**Files:**
- Create: `src/core/attachments.ts`
- Create: `test/attachments.test.ts`

**Interfaces:**
- Consumes: gateway route from Task 8.
- Produces (used by Tasks 12–13):
  - `export interface AttachmentMeta { id: string; filename: string; mime: string; size_bytes: number }`
  - `export interface FetchedAttachment extends AttachmentMeta { data: Uint8Array }`
  - `export async function fetchAttachment(coreUrl: string, token: string, meta: AttachmentMeta): Promise<FetchedAttachment>` — caches bytes at `<dataDir>/attachments/<id>`.

- [ ] **Step 1: Write the failing test**

`test/attachments.test.ts` (pattern from `test/provider-stream.test.ts`: real local `Bun.serve`):
```ts
import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { mkdtempSync } from "fs";
import { tmpdir } from "os";
import { join } from "path";
import { fetchAttachment, type AttachmentMeta } from "../src/core/attachments.ts";

let server: ReturnType<typeof Bun.serve>;
let hits = 0;
const bytes = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 1, 2, 3]);

beforeAll(() => {
  process.env.TRADEBRIDGE_AGENT_HOME = mkdtempSync(join(tmpdir(), "tb-agent-test-"));
  server = Bun.serve({
    port: 0,
    fetch(req) {
      const url = new URL(req.url);
      if (url.pathname === "/agent/v1/attachments/a1") {
        hits++;
        if (req.headers.get("authorization") !== "Bearer tok") {
          return new Response("unauthorized", { status: 401 });
        }
        return new Response(bytes, { headers: { "content-type": "image/png" } });
      }
      return new Response("not found", { status: 404 });
    },
  });
});

afterAll(() => server.stop(true));

const meta: AttachmentMeta = { id: "a1", filename: "x.png", mime: "image/png", size_bytes: 7 };

describe("fetchAttachment", () => {
  test("fetches bytes with bearer auth", async () => {
    const got = await fetchAttachment(`http://localhost:${server.port}`, "tok", meta);
    expect(got.data).toEqual(bytes);
    expect(got.mime).toBe("image/png");
    expect(hits).toBe(1);
  });

  test("second call is served from cache", async () => {
    const got = await fetchAttachment(`http://localhost:${server.port}`, "tok", meta);
    expect(got.data).toEqual(bytes);
    expect(hits).toBe(1); // no second network hit
  });

  test("throws on missing attachment", async () => {
    const missing = { ...meta, id: "nope" };
    expect(fetchAttachment(`http://localhost:${server.port}`, "tok", missing)).rejects.toThrow();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/deploy/b2b/tradebridge-agent && bun test test/attachments.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement**

`src/core/attachments.ts`:
```ts
// Fetches RFQ attachment bytes from the Core gateway with a local disk cache
// (attachments are immutable, so a cache hit never needs revalidation).

import { homedir } from "os";
import { join } from "path";
import { mkdir } from "fs/promises";

export interface AttachmentMeta {
  id: string;
  filename: string;
  mime: string;
  size_bytes: number;
}

export interface FetchedAttachment extends AttachmentMeta {
  data: Uint8Array;
}

function cacheDir(): string {
  const dataDir = process.env.TRADEBRIDGE_AGENT_HOME ?? join(homedir(), ".tradebridge-agent");
  return join(dataDir, "attachments");
}

export async function fetchAttachment(
  coreUrl: string,
  token: string,
  meta: AttachmentMeta,
): Promise<FetchedAttachment> {
  const path = join(cacheDir(), meta.id);
  const cached = Bun.file(path);
  if (await cached.exists()) {
    return { ...meta, data: new Uint8Array(await cached.arrayBuffer()) };
  }
  const res = await fetch(`${coreUrl}/agent/v1/attachments/${meta.id}`, {
    headers: { authorization: `Bearer ${token}` },
  });
  if (!res.ok) {
    throw new Error(`attachment fetch failed (${res.status}): ${await res.text()}`);
  }
  const data = new Uint8Array(await res.arrayBuffer());
  await mkdir(cacheDir(), { recursive: true });
  await Bun.write(path, data);
  return { ...meta, data };
}
```

- [ ] **Step 4: Run tests**

Run: `bun test test/attachments.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add src/core/attachments.ts test/attachments.test.ts
git commit -m "feat(core): fetch RFQ attachments from gateway with disk cache"
```

---

### Task 10: Agent — multimodal LLM providers

**Files:**
- Modify: `src/reasoning/provider.ts`
- Create: `test/provider-multimodal.test.ts`

**Interfaces:**
- Consumes: `extractPdfText` from `src/ingestion/extract.ts` (exists).
- Produces (used by Tasks 11–13):
  - `export interface LlmAttachment { filename: string; mime: string; data: string /* base64 */ }`
  - `CompletionRequest` gains `attachments?: LlmAttachment[]`
  - `export function anthropicContent(req: CompletionRequest): string | AnthropicBlock[]` (exported for tests)
  - `export async function openAiContent(req: CompletionRequest): Promise<string | unknown[]>` (exported for tests)
  - Both providers send attachments; `StubProvider` unchanged.

- [ ] **Step 1: Write failing tests**

`test/provider-multimodal.test.ts`:
```ts
import { describe, expect, test } from "bun:test";
import { anthropicContent, openAiContent, type CompletionRequest } from "../src/reasoning/provider.ts";

const png: CompletionRequest["attachments"] = [
  { filename: "photo.png", mime: "image/png", data: Buffer.from([1, 2, 3]).toString("base64") },
];

describe("anthropicContent", () => {
  test("no attachments → plain string", () => {
    expect(anthropicContent({ prompt: "hi" })).toBe("hi");
  });

  test("image becomes an image block before the text block", () => {
    const blocks = anthropicContent({ prompt: "quote this", attachments: png }) as any[];
    expect(blocks).toHaveLength(2);
    expect(blocks[0]).toEqual({
      type: "image",
      source: { type: "base64", media_type: "image/png", data: png![0].data },
    });
    expect(blocks[1]).toEqual({ type: "text", text: "quote this" });
  });

  test("pdf becomes a document block", () => {
    const pdf = [{ filename: "s.pdf", mime: "application/pdf", data: "JVBERg==" }];
    const blocks = anthropicContent({ prompt: "p", attachments: pdf }) as any[];
    expect(blocks[0].type).toBe("document");
    expect(blocks[0].source.media_type).toBe("application/pdf");
  });
});

describe("openAiContent", () => {
  test("no attachments → plain string", async () => {
    expect(await openAiContent({ prompt: "hi" })).toBe("hi");
  });

  test("image becomes a data-uri image_url part", async () => {
    const parts = (await openAiContent({ prompt: "p", attachments: png })) as any[];
    const img = parts.find((p) => p.type === "image_url");
    expect(img.image_url.url).toBe(`data:image/png;base64,${png![0].data}`);
    expect(parts.at(-1)).toEqual({ type: "text", text: "p" });
  });

  test("unreadable pdf degrades to an unavailable note", async () => {
    const bad = [{ filename: "broken.pdf", mime: "application/pdf", data: Buffer.from("nope").toString("base64") }];
    const parts = (await openAiContent({ prompt: "p", attachments: bad })) as any[];
    const text = parts.at(-1) as { type: string; text: string };
    expect(text.text).toContain("[attachment unavailable: broken.pdf]");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `bun test test/provider-multimodal.test.ts`
Expected: FAIL — `anthropicContent` not exported.

- [ ] **Step 3: Implement**

In `src/reasoning/provider.ts`:

1. Add types + extend `CompletionRequest`:
```ts
/** A file for the model to view. data is base64 (JSON-safe). */
export interface LlmAttachment {
  filename: string;
  mime: string; // image/* or application/pdf
  data: string;
}

export interface CompletionRequest {
  system?: string;
  prompt: string;
  attachments?: LlmAttachment[];
  maxTokens?: number;
  temperature?: number;
}
```
2. Add the content builders (top-level, exported):
```ts
type AnthropicBlock =
  | { type: "text"; text: string }
  | { type: "image"; source: { type: "base64"; media_type: string; data: string } }
  | { type: "document"; source: { type: "base64"; media_type: "application/pdf"; data: string } };

/** Anthropic Messages API content: attachment blocks first, then the text. */
export function anthropicContent(req: CompletionRequest): string | AnthropicBlock[] {
  if (!req.attachments?.length) return req.prompt;
  const blocks: AnthropicBlock[] = req.attachments.map((a) =>
    a.mime === "application/pdf"
      ? { type: "document" as const, source: { type: "base64" as const, media_type: "application/pdf" as const, data: a.data } }
      : { type: "image" as const, source: { type: "base64" as const, media_type: a.mime, data: a.data } },
  );
  blocks.push({ type: "text", text: req.prompt });
  return blocks;
}

/**
 * OpenAI-compatible content: images as data-URI image_url parts; PDFs have no
 * native block, so their text is extracted (unpdf) and appended to the prompt.
 */
export async function openAiContent(req: CompletionRequest): Promise<string | unknown[]> {
  if (!req.attachments?.length) return req.prompt;
  const { extractPdfText } = await import("../ingestion/extract.ts");
  let prompt = req.prompt;
  const parts: unknown[] = [];
  for (const a of req.attachments) {
    if (a.mime === "application/pdf") {
      try {
        const text = await extractPdfText(new Uint8Array(Buffer.from(a.data, "base64")));
        prompt += `\n\n--- Attached PDF "${a.filename}" (extracted text) ---\n${text.slice(0, 8000)}`;
      } catch {
        prompt += `\n\n[attachment unavailable: ${a.filename}]`;
      }
    } else {
      parts.push({ type: "image_url", image_url: { url: `data:${a.mime};base64,${a.data}` } });
    }
  }
  parts.push({ type: "text", text: prompt });
  return parts;
}
```
3. In `AnthropicProvider.complete` AND `completeStream`, change the messages line:
```ts
        messages: [{ role: "user", content: anthropicContent(req) }],
```
4. In `OpenAICompatibleProvider.complete` AND `completeStream`, change the user message construction:
```ts
    const userContent = await openAiContent(req);
    // ...
    messages.push({ role: "user", content: userContent });
```
(match the existing local structure — the system message handling stays as is).

- [ ] **Step 4: Run tests**

Run: `bun test test/provider-multimodal.test.ts test/provider.test.ts test/provider-stream.test.ts`
Expected: PASS (existing provider tests unchanged and green).

- [ ] **Step 5: Commit**

```bash
git add src/reasoning/provider.ts test/provider-multimodal.test.ts
git commit -m "feat(reasoning): multimodal content blocks in both LLM providers"
```

---

### Task 11: Agent — quoting with attachments + clarification thread in the prompt

**Files:**
- Modify: `src/reasoning/quote.ts`
- Create: `test/quote-attachments.test.ts`

**Interfaces:**
- Consumes: `LlmAttachment` (Task 10), `AttachmentMeta` (Task 9).
- Produces (used by Tasks 12–13):
  - `export interface ThreadMsg { author: "buyer" | "seller"; body: string }`
  - `QuoteInput` gains `attachments?: LlmAttachment[]`, `attachmentNotes?: string[]`, `thread?: ThreadMsg[]`, `mustQuote?: boolean`.
  - `RfqMatchedPayload` gains `attachments?: AttachmentMeta[]`.
  - `llmQuote` passes `attachments` into the `CompletionRequest`, renders the thread + notes in the prompt, and hardens the system prompt when `mustQuote` is set.

- [ ] **Step 1: Write failing tests**

`test/quote-attachments.test.ts` (uses the existing `llmQuoteForTest` seam and the mock-provider pattern from `test/quote-stream.test.ts`; check `llmQuoteForTest`'s exact parameter list in `src/reasoning/quote.ts:269-278` and mirror it):
```ts
import { describe, expect, test } from "bun:test";
import type { CompletionRequest } from "../src/reasoning/provider.ts";
import { llmQuoteForTest, type QuoteInput } from "../src/reasoning/quote.ts";

function capturingProvider(reply: string) {
  const seen: CompletionRequest[] = [];
  return {
    seen,
    provider: {
      complete: async (req: CompletionRequest) => {
        seen.push(req);
        return { text: reply, model: "mock" };
      },
    },
  };
}

const baseInput: QuoteInput = {
  sellerId: "s1",
  rfqId: "r1",
  title: "500 cartons",
  requirements: { category: "packaging" },
};

const quoteJson = `{"price": 100, "currency": "USD", "delivery_days": 5, "notes": "ok", "confidence": 0.9}`;

describe("llmQuote with attachments and thread", () => {
  test("attachments are forwarded to the provider request", async () => {
    const { seen, provider } = capturingProvider(quoteJson);
    const atts = [{ filename: "photo.png", mime: "image/png", data: "AAAA" }];
    await llmQuoteForTest(provider, { ...baseInput, attachments: atts }, [], [], undefined);
    expect(seen[0].attachments).toEqual(atts);
  });

  test("thread and unavailable-notes are rendered into the prompt", async () => {
    const { seen, provider } = capturingProvider(quoteJson);
    await llmQuoteForTest(
      provider,
      {
        ...baseInput,
        thread: [
          { author: "seller", body: "What size are the cartons?" },
          { author: "buyer", body: "40x40x40cm, see photo" },
        ],
        attachmentNotes: ["[attachment unavailable: broken.pdf]"],
      },
      [], [], undefined,
    );
    expect(seen[0].prompt).toContain("seller: What size are the cartons?");
    expect(seen[0].prompt).toContain("buyer: 40x40x40cm, see photo");
    expect(seen[0].prompt).toContain("[attachment unavailable: broken.pdf]");
  });

  test("mustQuote forbids clarification in the system prompt", async () => {
    const { seen, provider } = capturingProvider(quoteJson);
    await llmQuoteForTest(provider, { ...baseInput, mustQuote: true }, [], [], undefined);
    expect(seen[0].system).toContain("do NOT ask another clarification");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `bun test test/quote-attachments.test.ts`
Expected: FAIL — new fields don't exist / prompt lacks the sections.

- [ ] **Step 3: Implement in `src/reasoning/quote.ts`**

1. Types:
```ts
import type { LlmAttachment } from "./provider.ts";
import type { AttachmentMeta } from "../core/attachments.ts";

export interface ThreadMsg {
  author: "buyer" | "seller";
  body: string;
}
```
   - `RfqMatchedPayload` gains `attachments?: AttachmentMeta[];`
   - `QuoteInput` gains:
```ts
  attachments?: LlmAttachment[];
  /** e.g. "[attachment unavailable: x.pdf]" for files that failed to fetch */
  attachmentNotes?: string[];
  thread?: ThreadMsg[];
  /** clarification cap reached: the model must price or decline, not ask */
  mustQuote?: boolean;
```
2. In `llmQuote`, extend the prompt after the catalog context:
```ts
  const threadSection = input.thread?.length
    ? `\n\nClarification thread so far (answer is in the buyer messages):\n${input.thread
        .map((m) => `${m.author}: ${m.body}`)
        .join("\n")}`
    : "";
  const notesSection = input.attachmentNotes?.length ? `\n\n${input.attachmentNotes.join("\n")}` : "";

  const prompt = `RFQ: ${input.title}
Requirements: ${JSON.stringify(input.requirements)}

Catalog context:
${context || "(no direct catalog matches)"}${threadSection}${notesSection}`;
```
3. Harden the system prompt when capped — append after the existing JSON contract text:
```ts
  const system = `...existing template...${
    input.mustQuote
      ? "\nYou have already asked the maximum number of clarifying questions — do NOT ask another clarification. Return a priced quote using your best assumptions (state them in notes)."
      : ""
  }`;
```
4. Pass attachments through to the provider call:
```ts
  const res = await completeStreaming(provider, { system, prompt, attachments: input.attachments, maxTokens: 512 }, fs.feed);
```
(mirror in the non-stream path if there is one.)

- [ ] **Step 4: Run tests**

Run: `bun test test/quote-attachments.test.ts test/quote.test.ts test/quote-stream.test.ts test/quote-catalog.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/reasoning/quote.ts test/quote-attachments.test.ts
git commit -m "feat(reasoning): quote with attachments, thread context, and mustQuote cap"
```

---

### Task 12: Agent — Rfq entity thread state + rfq.matched handler fetches attachments

**Files:**
- Modify: `src/db/entities/Rfq.ts` (3 new columns)
- Modify: `src/rfq/index.ts` (`UpsertRfqActiveInput` + upsert)
- Modify: `src/reasoning/handler.ts` (fetch attachments, record thread + rounds)
- Create: `test/rfq-thread-state.test.ts`

**Interfaces:**
- Consumes: `fetchAttachment`/`AttachmentMeta` (Task 9), `LlmAttachment` (Task 10), `ThreadMsg` (Task 11).
- Produces (used by Task 13):
  - `Rfq` entity columns: `attachments: AttachmentMeta[] | null` (simple-json), `thread: ThreadMsg[] | null` (simple-json), `clarificationRounds: number` (int, default 0).
  - `UpsertRfqActiveInput` gains optional `attachments?`, `thread?`, `clarificationRounds?` (only assigned when provided).
  - `export async function loadLlmAttachments(metas: AttachmentMeta[] | undefined, deps: HandlerDeps): Promise<{ atts: LlmAttachment[]; notes: string[] }>` in `src/reasoning/handler.ts`.

- [ ] **Step 1: Extend the entity**

`src/db/entities/Rfq.ts` — add columns (with imports `import type { AttachmentMeta } from "../../core/attachments.ts"; import type { ThreadMsg } from "../../reasoning/quote.ts";` — if this creates an import cycle, define the two shapes inline as local interfaces instead):
```ts
  @Column("simple-json", { name: "attachments", nullable: true })
  attachments!: AttachmentMeta[] | null;

  @Column("simple-json", { name: "thread", nullable: true })
  thread!: ThreadMsg[] | null;

  @Column("int", { name: "clarification_rounds", default: 0 })
  clarificationRounds!: number;
```
(`synchronize: true` applies this automatically — no migration.)

- [ ] **Step 2: Extend the upsert**

`src/rfq/index.ts`:
```ts
export interface UpsertRfqActiveInput {
  sellerId: string;
  rfqId: string;
  title: string;
  category: string;
  requirements: Record<string, unknown> | null;
  quoteState: QuoteState;
  price?: number | null;
  currency?: string | null;
  pendingQuoteId?: string | null;
  attachments?: AttachmentMeta[] | null;
  thread?: ThreadMsg[] | null;
  clarificationRounds?: number;
}
```
and in `upsertRfqActive`, after the existing assignments:
```ts
  if (input.attachments !== undefined) row.attachments = input.attachments;
  if (input.thread !== undefined) row.thread = input.thread;
  if (input.clarificationRounds !== undefined) row.clarificationRounds = input.clarificationRounds;
```
(import the types from the same place the entity does.)

- [ ] **Step 3: Write failing state test**

`test/rfq-thread-state.test.ts` (in-memory DB via env, pattern: set `AGENT_DB=:memory:`-equivalent — check how other DB-touching tests do it; if none do, use `TRADEBRIDGE_AGENT_HOME` pointed at a temp dir):
```ts
import { beforeAll, describe, expect, test } from "bun:test";
import { mkdtempSync } from "fs";
import { tmpdir } from "os";
import { join } from "path";

beforeAll(() => {
  process.env.TRADEBRIDGE_AGENT_HOME = mkdtempSync(join(tmpdir(), "tb-agent-db-"));
});

describe("rfq thread state", () => {
  test("upsert stores attachments, thread and rounds; second upsert preserves them", async () => {
    const { upsertRfqActive } = await import("../src/rfq/index.ts");
    const { getDataSource, Rfq } = await import("../src/db/index.ts");

    await upsertRfqActive({
      sellerId: "s1", rfqId: "r-att-1", title: "t", category: "c",
      requirements: null, quoteState: "clarification",
      attachments: [{ id: "a1", filename: "x.png", mime: "image/png", size_bytes: 5 }],
      thread: [{ author: "seller", body: "what size?" }],
      clarificationRounds: 1,
    });

    // A later upsert without those fields must not wipe them.
    await upsertRfqActive({
      sellerId: "s1", rfqId: "r-att-1", title: "t", category: "c",
      requirements: null, quoteState: "clarification",
    });

    const ds = await getDataSource();
    const row = await ds.getRepository(Rfq).findOneByOrFail({ rfqId: "r-att-1" });
    expect(row.attachments?.[0]?.id).toBe("a1");
    expect(row.thread).toHaveLength(1);
    expect(row.clarificationRounds).toBe(1);
  });
});
```

- [ ] **Step 4: Run tests**

Run: `bun test test/rfq-thread-state.test.ts`
Expected: FAIL before Steps 1–2 are complete, PASS after.

- [ ] **Step 5: Wire attachments into handleRfqMatched**

`src/reasoning/handler.ts`:

1. Add the fetch helper:
```ts
import { fetchAttachment, type AttachmentMeta } from "../core/attachments.ts";
import type { LlmAttachment } from "./provider.ts";

/** Fetch attachment bytes; failures degrade to an unavailable-note. */
export async function loadLlmAttachments(
  metas: AttachmentMeta[] | undefined,
  deps: HandlerDeps,
): Promise<{ atts: LlmAttachment[]; notes: string[] }> {
  const atts: LlmAttachment[] = [];
  const notes: string[] = [];
  for (const m of metas ?? []) {
    try {
      const f = await fetchAttachment(deps.coreUrl, deps.token, m);
      atts.push({ filename: f.filename, mime: f.mime, data: Buffer.from(f.data).toString("base64") });
    } catch (err) {
      notes.push(`[attachment unavailable: ${m.filename}]`);
      await logAgent("warn", "attachment.fetch_failed", { id: m.id, error: (err as Error).message });
    }
  }
  return { atts, notes };
}
```
2. In `handleRfqMatched`, before `generateQuote`:
```ts
    const { atts, notes } = await loadLlmAttachments(ev.attachments, deps);

    const decision = await generateQuote(
      { sellerId, rfqId: ev.rfq_id, title: ev.title, requirements, attachments: atts, attachmentNotes: notes },
      (delta) => publishActivity({ type: "quote.thinking", rfqId: ev.rfq_id, title: ev.title, delta }),
    );
```
3. In the clarification branch, record the thread + round + attachment metas:
```ts
      await upsertRfqActive({
        sellerId, rfqId: ev.rfq_id, title: ev.title, category: requirements.category ?? "",
        requirements: requirements as Record<string, unknown>, quoteState: "clarification",
        attachments: ev.attachments ?? null,
        thread: [{ author: "seller", body: decision.question }],
        clarificationRounds: 1,
      });
```
4. In the queued and submitted branches, also persist `attachments: ev.attachments ?? null` (so a later `quote.message` can reuse them).

- [ ] **Step 6: Run the full agent suite**

Run: `bun test && bunx tsc --noEmit`
Expected: PASS / no type errors.

- [ ] **Step 7: Commit**

```bash
git add src test/rfq-thread-state.test.ts
git commit -m "feat(agent): rfq thread state + vision attachments on rfq.matched"
```

---

### Task 13: Agent — handle `quote.message` (re-quote with thread, enforce cap)

**Files:**
- Modify: `src/reasoning/handler.ts` (add `handleQuoteMessage`)
- Modify: `src/runtime.ts` (dispatch the event)
- Modify: `src/core/commands.ts` (only if `no_bid` payload needs a message field — it already supports type `no_bid`)
- Create: `test/quote-message.test.ts`

**Interfaces:**
- Consumes: everything from Tasks 9–12; `quote.message` payload from core Task 7: `{type, rfq_id, quote_id, author, body, attachments?}`.
- Produces:
  - `export interface QuoteMessagePayload { type: string; rfq_id: string; quote_id: string; author: "buyer" | "seller"; body: string; attachments?: AttachmentMeta[] }`
  - `export function decideCap(decision: QuoteDecision, rounds: number): "quote" | "clarify" | "no_bid"` (pure, exported for tests)
  - `export async function handleQuoteMessage(payloadJson: string, deps: HandlerDeps): Promise<HandlerResult | null>` — null when the message is not actionable (seller-authored, unknown RFQ, resolved RFQ).

- [ ] **Step 1: Write failing tests for the pure cap logic**

`test/quote-message.test.ts`:
```ts
import { describe, expect, test } from "bun:test";
import { decideCap } from "../src/reasoning/handler.ts";

const clarify = { kind: "clarification" as const, question: "what color?" };
const quote = {
  kind: "quote" as const,
  quote: { price: 10, currency: "USD", notes: "", confidence: 0.8 },
};

describe("decideCap", () => {
  test("quote decisions always pass through", () => {
    expect(decideCap(quote, 0)).toBe("quote");
    expect(decideCap(quote, 5)).toBe("quote");
  });

  test("clarification allowed under the 2-round cap", () => {
    expect(decideCap(clarify, 0)).toBe("clarify");
    expect(decideCap(clarify, 1)).toBe("clarify");
  });

  test("clarification at/over the cap becomes no_bid", () => {
    expect(decideCap(clarify, 2)).toBe("no_bid");
    expect(decideCap(clarify, 3)).toBe("no_bid");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `bun test test/quote-message.test.ts`
Expected: FAIL — `decideCap` not exported.

- [ ] **Step 3: Implement `handleQuoteMessage`**

In `src/reasoning/handler.ts`:
```ts
import { getDataSource } from "../db/index.ts";
import { Rfq } from "../db/entities/Rfq.ts";
import type { QuoteDecision, ThreadMsg } from "./quote.ts";

export interface QuoteMessagePayload {
  type: string;
  rfq_id: string;
  quote_id: string;
  author: "buyer" | "seller";
  body: string;
  attachments?: AttachmentMeta[];
}

export const MAX_CLARIFICATION_ROUNDS = 2;

/** Pure cap rule: at the cap, a clarification decision becomes a no_bid. */
export function decideCap(decision: QuoteDecision, rounds: number): "quote" | "clarify" | "no_bid" {
  if (decision.kind === "quote") return "quote";
  return rounds < MAX_CLARIFICATION_ROUNDS ? "clarify" : "no_bid";
}

/**
 * A buyer replied in the quote thread (usually answering our clarification).
 * Re-run the quote decision with the full thread + all attachments.
 */
export async function handleQuoteMessage(
  payloadJson: string,
  deps: HandlerDeps,
): Promise<HandlerResult | null> {
  const ev = JSON.parse(payloadJson) as QuoteMessagePayload;
  if (ev.author !== "buyer") return null;

  const ds = await getDataSource();
  const repo = ds.getRepository(Rfq);
  const row = await repo.findOne({ where: { rfqId: ev.rfq_id } });
  if (!row) {
    await logAgent("info", "quote_message.skip", { rfqId: ev.rfq_id, reason: "unknown rfq" });
    return null;
  }
  if (row.status !== "active") {
    await logAgent("info", "quote_message.skip", { rfqId: ev.rfq_id, reason: `already ${row.status}` });
    return null;
  }

  // Merge newly attached files into the RFQ's known attachments (dedup by id).
  const known = row.attachments ?? [];
  const merged = [...known, ...(ev.attachments ?? []).filter((a) => !known.some((k) => k.id === a.id))];
  const thread: ThreadMsg[] = [...(row.thread ?? []), { author: "buyer", body: ev.body }];
  const rounds = row.clarificationRounds ?? 0;

  const { atts, notes } = await loadLlmAttachments(merged, deps);
  const requirements = (row.requirements ?? {}) as RfqMatchedPayload["requirements"];

  const decision = await generateQuote(
    {
      sellerId: row.sellerId, rfqId: ev.rfq_id, title: row.title, requirements,
      attachments: atts, attachmentNotes: notes, thread,
      mustQuote: rounds >= MAX_CLARIFICATION_ROUNDS,
    },
    (delta) => publishActivity({ type: "quote.thinking", rfqId: ev.rfq_id, title: row.title, delta }),
  );

  const action = decideCap(decision, rounds);

  if (action === "no_bid") {
    await sendCommand(deps.coreUrl, deps.token, {
      type: "no_bid",
      idempotencyKey: `nobid-${ev.rfq_id}`,
      rfqId: ev.rfq_id,
      payload: { reason: "could not price after maximum clarification rounds" },
    });
    await markRfqOutcome(ev.rfq_id, "lost");
    await logAgent("info", "quote_message.no_bid", { rfqId: ev.rfq_id, rounds });
    return { rfqId: ev.rfq_id, action: "clarification.request", detail: "no_bid: clarification cap reached" };
  }

  if (action === "clarify" && decision.kind === "clarification") {
    await sendCommand(deps.coreUrl, deps.token, {
      type: "clarification.request",
      idempotencyKey: `clar-${ev.rfq_id}-${rounds + 1}`,
      rfqId: ev.rfq_id,
      payload: { message: decision.question },
    });
    await upsertRfqActive({
      sellerId: row.sellerId, rfqId: ev.rfq_id, title: row.title, category: row.category,
      requirements: requirements as Record<string, unknown>, quoteState: "clarification",
      attachments: merged,
      thread: [...thread, { author: "seller", body: decision.question }],
      clarificationRounds: rounds + 1,
    });
    return { rfqId: ev.rfq_id, action: "clarification.request", detail: decision.question };
  }

  // action === "quote": same autonomy gate as handleRfqMatched.
  if (decision.kind !== "quote") return null; // type narrowing safety
  const policy = await getPolicy(row.sellerId);
  const gate = evaluatePolicy(policy, decision.quote, requirements.category);
  if (!gate.allow) {
    const queued = await queuePending(row.sellerId, ev.rfq_id, row.title, decision.quote, gate.reasons);
    await upsertRfqActive({
      sellerId: row.sellerId, rfqId: ev.rfq_id, title: row.title, category: row.category,
      requirements: requirements as Record<string, unknown>, quoteState: "queued",
      price: decision.quote.price, currency: decision.quote.currency,
      pendingQuoteId: queued.id, attachments: merged, thread,
    });
    return { rfqId: ev.rfq_id, action: "queued", detail: gate.reasons.join("; ") };
  }
  await sendCommand(deps.coreUrl, deps.token, {
    type: "quote.submit",
    idempotencyKey: `quote-${ev.rfq_id}-r${rounds}`,
    rfqId: ev.rfq_id,
    payload: {
      price: decision.quote.price,
      currency: decision.quote.currency,
      delivery_days: decision.quote.deliveryDays,
      notes: decision.quote.notes,
    },
  });
  await upsertRfqActive({
    sellerId: row.sellerId, rfqId: ev.rfq_id, title: row.title, category: row.category,
    requirements: requirements as Record<string, unknown>, quoteState: "submitted",
    price: decision.quote.price, currency: decision.quote.currency,
    attachments: merged, thread,
  });
  return { rfqId: ev.rfq_id, action: "quote.submit", detail: `${decision.quote.currency} ${decision.quote.price}` };
}
```
(Reuse existing imports — `getPolicy`, `evaluatePolicy`, `queuePending`, `markRfqOutcome` is imported from `../rfq/index.ts`.)

- [ ] **Step 4: Dispatch in the runtime**

`src/runtime.ts` — add to the `processEvent` chain (after the `rfq.matched` case):
```ts
    } else if (ev.type === "quote.message" && deps.autoQuote) {
      await handleQuoteMessage(ev.data, { coreUrl: deps.coreUrl, token: deps.token });
```
and import it: `import { handleRfqMatched, handleQuoteMessage } from "./reasoning/handler.ts";`

- [ ] **Step 5: Run full suite + typecheck**

Run: `bun test && bunx tsc --noEmit`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add src test/quote-message.test.ts
git commit -m "feat(agent): handle quote.message — re-quote with thread, 2-round clarification cap"
```

---

### Task 14: Web — API client for attachments + quote messages, reducer support

**Files:**
- Modify: `web/lib/api.ts`
- Create: `web/lib/attachments.ts`
- Create: `web/lib/attachments.test.ts`
- Modify: `web/lib/chat.ts` + `web/lib/chat.test.ts`

**Interfaces:**
- Consumes: Core endpoints from Tasks 3 & 7.
- Produces (used by Tasks 15–16):
  - `export type AttachmentMeta = { id: string; filename: string; mime: string; size_bytes: number }`
  - `ChatMessage` gains `attachment_ids?: string[]; attachments?: AttachmentMeta[]`
  - `uploadAttachment(token: string, file: File): Promise<AttachmentMeta>`
  - `fetchAttachmentBlob(token: string, id: string): Promise<Blob>`
  - `quoteMessages(token, quoteId): Promise<{messages: QuoteMessage[]}>`, `postQuoteMessage(token, quoteId, body, attachmentIds): Promise<QuoteMessage>` where `QuoteMessage = { id: string; author: "buyer" | "seller"; body: string; attachments: AttachmentMeta[] | null; created_at: string }`
  - `web/lib/attachments.ts`: `validateFile(name: string, type: string, size: number): string | null` (pure; returns an error message or null), `MAX_FILE_BYTES`, `MAX_PER_MESSAGE`, `ACCEPT` constant `"image/jpeg,image/png,image/webp,image/gif,application/pdf"`.
  - Chat reducer `send` action gains `attachmentIds?: string[]; attachments?: AttachmentMeta[]`, stored on the appended user message.

- [ ] **Step 1: Write failing lib tests**

`web/lib/attachments.test.ts`:
```ts
import { describe, expect, it } from "vitest";
import { validateFile, MAX_FILE_BYTES } from "./attachments";

describe("validateFile", () => {
  it("accepts a small png", () => {
    expect(validateFile("a.png", "image/png", 1024)).toBeNull();
  });
  it("accepts a pdf", () => {
    expect(validateFile("spec.pdf", "application/pdf", 1024)).toBeNull();
  });
  it("rejects disallowed types", () => {
    expect(validateFile("a.zip", "application/zip", 10)).toMatch(/not supported/i);
  });
  it("rejects oversize files", () => {
    expect(validateFile("big.png", "image/png", MAX_FILE_BYTES + 1)).toMatch(/10 MB/);
  });
});
```

Extend `web/lib/chat.test.ts` with (follow the file's existing style):
```ts
it("send action carries attachments onto the user message", () => {
  const s = chatReducer(initialChatState, {
    type: "send",
    content: "here",
    attachmentIds: ["a1"],
    attachments: [{ id: "a1", filename: "x.png", mime: "image/png", size_bytes: 5 }],
  });
  const last = s.messages.at(-1)!;
  expect(last.attachment_ids).toEqual(["a1"]);
  expect(last.attachments?.[0]?.filename).toBe("x.png");
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/deploy/b2b/tradebridge-core/web && npm test`
Expected: FAIL — module/fields missing.

- [ ] **Step 3: Implement**

`web/lib/attachments.ts`:
```ts
// Client-side attachment validation, mirroring Core's server-side limits.

export const MAX_FILE_BYTES = 10 * 1024 * 1024;
export const MAX_PER_MESSAGE = 5;
export const ACCEPT = "image/jpeg,image/png,image/webp,image/gif,application/pdf";

const allowed = new Set(ACCEPT.split(","));

/** Returns an error message, or null when the file is acceptable. */
export function validateFile(name: string, type: string, size: number): string | null {
  if (!allowed.has(type)) return `${name}: file type not supported (images and PDFs only)`;
  if (size > MAX_FILE_BYTES) return `${name}: larger than the 10 MB limit`;
  return null;
}
```

`web/lib/api.ts` additions:
```ts
export type AttachmentMeta = {
  id: string;
  filename: string;
  mime: string;
  size_bytes: number;
};

export type ChatMessage = {
  role: "user" | "assistant";
  content: string;
  attachment_ids?: string[];
  attachments?: AttachmentMeta[];
};

export type QuoteMessage = {
  id: string;
  author: "buyer" | "seller";
  body: string;
  attachments: AttachmentMeta[] | null;
  created_at: string;
};

// Multipart upload — bypasses req() because that wrapper is JSON-only.
export async function uploadAttachment(token: string, file: File): Promise<AttachmentMeta> {
  const form = new FormData();
  form.append("file", file, file.name);
  const res = await fetch(`${CORE_URL}/v1/attachments`, {
    method: "POST",
    headers: { authorization: `Bearer ${token}` },
    body: form,
    cache: "no-store",
  });
  if (!res.ok) throw new Error((await res.text()) || `upload failed (${res.status})`);
  return res.json();
}

// Downloads need the auth header, so <img src> can't hit the URL directly —
// fetch a Blob and object-URL it in the component.
export async function fetchAttachmentBlob(token: string, id: string): Promise<Blob> {
  const res = await fetch(`${CORE_URL}/v1/attachments/${id}`, {
    headers: { authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  if (!res.ok) throw new Error(`attachment fetch failed (${res.status})`);
  return res.blob();
}

export function quoteMessages(token: string, quoteId: string) {
  return req<{ messages: QuoteMessage[]; count: number }>(`/v1/quotes/${quoteId}/messages`, { token });
}

export function postQuoteMessage(token: string, quoteId: string, body: string, attachmentIds: string[]) {
  return req<QuoteMessage>(`/v1/quotes/${quoteId}/messages`, {
    method: "POST",
    token,
    body: { body, attachment_ids: attachmentIds },
  });
}
```
(Match `req`'s actual option shape — look at how `rfqQuotes`/`acceptQuote` call it and follow suit.)

`web/lib/chat.ts` — extend the send action and reducer:
```ts
// in the action union:
  | { type: "send"; content: string; attachmentIds?: string[]; attachments?: AttachmentMeta[] }

// in the reducer's "send" case, build the user message as:
        {
          role: "user",
          content: action.content,
          ...(action.attachmentIds?.length
            ? { attachment_ids: action.attachmentIds, attachments: action.attachments }
            : {}),
        }
```
(import `AttachmentMeta` from `./api`.)

- [ ] **Step 4: Run tests + lint**

Run: `npm test && npm run lint`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/lib
git commit -m "feat(web): attachment upload/download client, quote-message client, reducer support"
```

---

### Task 15: Web — attachment composer + RFQ chat integration + transcript rendering

**Files:**
- Create: `web/components/Composer.tsx` (moved + extended from `app/(chat)/chat/page.tsx`)
- Create: `web/components/AttachmentView.tsx`
- Modify: `web/app/(chat)/chat/page.tsx`
- Modify: `web/app/(chat)/chat/[rfqId]/page.tsx` (render attachments in transcript bubbles)

**Interfaces:**
- Consumes: Task 14 (`uploadAttachment`, `fetchAttachmentBlob`, `validateFile`, `ACCEPT`, `MAX_PER_MESSAGE`, `AttachmentMeta`, reducer fields).
- Produces:
  - `<Composer draft setDraft onSend={(attachments: AttachmentMeta[]) => void|Promise<void>} disabled token placeholder?>` — owns file selection + upload; calls `onSend` with the uploaded metas after uploading on submit; the old `Composer` export in `chat/page.tsx` is removed.
  - `<AttachmentView meta={AttachmentMeta} token={string}>` — image thumbnail (max-height 160px) or PDF chip link; used by Tasks 15–16.

- [ ] **Step 1: Build AttachmentView**

`web/components/AttachmentView.tsx`:
```tsx
"use client";
import { useEffect, useState } from "react";
import { fetchAttachmentBlob, type AttachmentMeta } from "../lib/api";

// Renders one attachment: images inline, PDFs as a chip that opens in a new
// tab. Fetches via authenticated fetch → object URL (plain <img src> can't
// send the bearer token).
export function AttachmentView({ meta, token }: { meta: AttachmentMeta; token: string }) {
  const [url, setUrl] = useState<string | null>(null);
  const [err, setErr] = useState(false);

  useEffect(() => {
    let obj: string | null = null;
    let alive = true;
    fetchAttachmentBlob(token, meta.id)
      .then((blob) => {
        obj = URL.createObjectURL(blob);
        if (alive) setUrl(obj);
      })
      .catch(() => alive && setErr(true));
    return () => {
      alive = false;
      if (obj) URL.revokeObjectURL(obj);
    };
  }, [meta.id, token]);

  if (err) return <span className="muted">⚠ {meta.filename} unavailable</span>;
  if (!url) return <span className="muted">… {meta.filename}</span>;

  if (meta.mime.startsWith("image/")) {
    return (
      <a href={url} target="_blank" rel="noreferrer">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={url}
          alt={meta.filename}
          style={{ maxHeight: 160, maxWidth: "100%", borderRadius: 8, border: "1px solid var(--border)" }}
        />
      </a>
    );
  }
  return (
    <a className="badge badge-info" href={url} target="_blank" rel="noreferrer">
      📄 {meta.filename}
    </a>
  );
}
```

- [ ] **Step 2: Build the shared Composer with attachments**

`web/components/Composer.tsx`:
```tsx
"use client";
import { useRef, useState } from "react";
import { uploadAttachment, type AttachmentMeta } from "../lib/api";
import { ACCEPT, MAX_PER_MESSAGE, validateFile } from "../lib/attachments";

export function Composer({
  draft,
  setDraft,
  onSend,
  disabled,
  token,
  placeholder = "Describe what you need…",
}: {
  draft: string;
  setDraft: (v: string) => void;
  onSend: (attachments: AttachmentMeta[]) => void | Promise<void>;
  disabled: boolean;
  token: string;
  placeholder?: string;
}) {
  const [files, setFiles] = useState<File[]>([]);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const pick = (list: FileList | null) => {
    if (!list) return;
    setError(null);
    const next = [...files];
    for (const f of Array.from(list)) {
      const problem = validateFile(f.name, f.type, f.size);
      if (problem) {
        setError(problem);
        continue;
      }
      if (next.length >= MAX_PER_MESSAGE) {
        setError(`Max ${MAX_PER_MESSAGE} attachments per message`);
        break;
      }
      next.push(f);
    }
    setFiles(next);
    if (fileRef.current) fileRef.current.value = "";
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (disabled || uploading || (!draft.trim() && files.length === 0)) return;
    setError(null);
    setUploading(true);
    try {
      const metas: AttachmentMeta[] = [];
      for (const f of files) metas.push(await uploadAttachment(token, f));
      setFiles([]);
      await onSend(metas);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Upload failed — retry");
    } finally {
      setUploading(false);
    }
  };

  return (
    <form onSubmit={submit} style={{ borderTop: "1px solid var(--border)", padding: "0.9rem 1.5rem" }}>
      <div className="col" style={{ gap: "0.5rem", maxWidth: 760, margin: "0 auto" }}>
        {files.length > 0 && (
          <div className="row" style={{ gap: "0.4rem", flexWrap: "wrap" }}>
            {files.map((f, i) => (
              <span key={`${f.name}-${i}`} className="badge badge-info">
                {f.type === "application/pdf" ? "📄" : "🖼"} {f.name}
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  aria-label={`remove ${f.name}`}
                  onClick={() => setFiles(files.filter((_, j) => j !== i))}
                >
                  ✕
                </button>
              </span>
            ))}
          </div>
        )}
        {error && <div className="err">{error}</div>}
        <div className="row" style={{ gap: "0.6rem" }}>
          <input
            ref={fileRef}
            type="file"
            accept={ACCEPT}
            multiple
            style={{ display: "none" }}
            onChange={(e) => pick(e.target.files)}
          />
          <button
            type="button"
            className="btn btn-ghost"
            aria-label="attach files"
            title="Attach images or PDFs"
            onClick={() => fileRef.current?.click()}
            disabled={disabled || uploading}
          >
            📎
          </button>
          <input
            className="input"
            style={{ flex: 1 }}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={placeholder}
            autoFocus
          />
          <button className="btn btn-primary" disabled={disabled || uploading || (!draft.trim() && files.length === 0)}>
            {uploading ? "Uploading…" : "Send"}
          </button>
        </div>
      </div>
    </form>
  );
}
```

- [ ] **Step 3: Integrate into the RFQ chat page**

`web/app/(chat)/chat/page.tsx`:
1. Delete the local `Composer` and import the new one: `import { Composer } from "../../../components/Composer";` (fix the relative path to match the file tree).
2. Change the send handler to accept metas and include IDs in the outgoing message:
```tsx
  const send = async (attachments: AttachmentMeta[]) => {
    const content = draft.trim();
    if ((!content && attachments.length === 0) || !token || state.pending) return;
    const ids = attachments.map((a) => a.id);
    const next = [
      ...state.messages,
      {
        role: "user" as const,
        content,
        ...(ids.length ? { attachment_ids: ids, attachments } : {}),
      },
    ];
    dispatch({ type: "send", content, attachmentIds: ids, attachments });
    setDraft("");
    try {
      const res = await rfqChatStream(token, next, (text) => dispatch({ type: "delta", text }));
      if (res.done && res.rfq) {
        router.push(`/chat/${res.rfq.id}`);
        return;
      }
      dispatch({ type: "reply", assistant: res.assistant });
    } catch (err) {
      dispatch({ type: "error", error: err instanceof Error ? err.message : "Couldn't reach Core — retry" });
    }
  };
```
3. Usage: `<Composer draft={draft} setDraft={setDraft} onSend={send} disabled={state.pending} token={token ?? ""} />`
4. In the message-bubble rendering on this page, under each user message with attachments, render chips:
```tsx
  {m.attachments?.map((a) => (
    <div key={a.id} style={{ marginTop: "0.35rem" }}>
      <AttachmentView meta={a} token={token ?? ""} />
    </div>
  ))}
```

- [ ] **Step 4: Render attachments in the saved transcript**

`web/app/(chat)/chat/[rfqId]/page.tsx` — the `rfq.messages` bubble loop gains the same `AttachmentView` block as Step 3.4 (the Go API now returns `attachments` metadata on persisted messages).

- [ ] **Step 5: Verify**

Run: `npm run lint && npm test && npm run build`
Expected: all PASS; build succeeds.
Manual check (optional if core dev stack is running): `npm run dev`, open `http://localhost:3010/chat`, attach a PNG, send, confirm the chip renders and the RFQ is created.

- [ ] **Step 6: Commit**

```bash
git add web
git commit -m "feat(web): attachment composer + transcript rendering in RFQ chat"
```

---

### Task 16: Web — quote thread UI (messages list + reply with attachments)

**Files:**
- Create: `web/components/QuoteThread.tsx`
- Modify: `web/app/(chat)/chat/[rfqId]/page.tsx` (mount per quote)

**Interfaces:**
- Consumes: `quoteMessages`, `postQuoteMessage`, `QuoteMessage` (Task 14), `Composer`, `AttachmentView` (Task 15).
- Produces: `<QuoteThread quoteId token />` — polls messages every 4s (same cadence the page already uses for quotes), renders buyer/seller bubbles with attachments, and a reply Composer.

- [ ] **Step 1: Build QuoteThread**

`web/components/QuoteThread.tsx`:
```tsx
"use client";
import { useCallback, useEffect, useState } from "react";
import { postQuoteMessage, quoteMessages, type AttachmentMeta, type QuoteMessage } from "../lib/api";
import { AttachmentView } from "./AttachmentView";
import { Composer } from "./Composer";

// The buyer↔seller-agent thread under one quote: where the agent's clarifying
// questions appear and the buyer answers (optionally with images/PDFs).
export function QuoteThread({ quoteId, token }: { quoteId: string; token: string }) {
  const [messages, setMessages] = useState<QuoteMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const res = await quoteMessages(token, quoteId);
      setMessages(res.messages);
    } catch {
      /* transient poll errors are fine */
    }
  }, [quoteId, token]);

  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 4000);
    return () => clearInterval(t);
  }, [refresh]);

  const send = async (attachments: AttachmentMeta[]) => {
    const body = draft.trim();
    if (!body && attachments.length === 0) return;
    setSending(true);
    setError(null);
    try {
      await postQuoteMessage(token, quoteId, body, attachments.map((a) => a.id));
      setDraft("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Couldn't send — retry");
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="col" style={{ gap: "0.5rem", marginTop: "0.6rem" }}>
      {messages.map((m) => (
        <div
          key={m.id}
          className="card"
          style={{
            padding: "0.5rem 0.75rem",
            alignSelf: m.author === "buyer" ? "flex-end" : "flex-start",
            maxWidth: "85%",
          }}
        >
          <div className="muted" style={{ fontSize: "0.75rem" }}>
            {m.author === "buyer" ? "You" : "Seller agent"}
          </div>
          {m.body && <div>{m.body}</div>}
          {m.attachments?.map((a) => (
            <div key={a.id} style={{ marginTop: "0.35rem" }}>
              <AttachmentView meta={a} token={token} />
            </div>
          ))}
        </div>
      ))}
      {messages.length === 0 && <div className="muted">No messages yet.</div>}
      {error && <div className="err">{error}</div>}
      <Composer
        draft={draft}
        setDraft={setDraft}
        onSend={send}
        disabled={sending}
        token={token}
        placeholder="Reply to the seller agent…"
      />
    </div>
  );
}
```

- [ ] **Step 2: Mount it under each quote**

`web/app/(chat)/chat/[rfqId]/page.tsx` — in the Quotes card, inside the per-quote render, add a collapsible thread (state: `openThread: string | null`):
```tsx
  <button className="btn btn-ghost btn-sm" onClick={() => setOpenThread(openThread === q.id ? null : q.id)}>
    💬 Messages
  </button>
  {openThread === q.id && token && <QuoteThread quoteId={q.id} token={token} />}
```
(adapt variable names to the page's existing quote-map loop.)

- [ ] **Step 3: Verify**

Run: `npm run lint && npm test && npm run build`
Expected: PASS.
Manual (optional, needs the full dev stack + an agent): create an RFQ that triggers a clarification, open the thread, reply with an image, and watch the agent submit a quote after the `quote.message` event.

- [ ] **Step 4: Commit**

```bash
git add web
git commit -m "feat(web): quote thread UI — clarification messages + reply with attachments"
```

---

## Final verification (after all tasks)

- [ ] Core: `cd /home/deploy/b2b/tradebridge-core && go build ./... && go test ./...` → PASS
- [ ] Core integration (if DB up): `TEST_DATABASE_URL=... go test ./test/integration/ -v` → PASS
- [ ] Agent: `cd /home/deploy/b2b/tradebridge-agent && bun test && bunx tsc --noEmit` → PASS
- [ ] Web: `cd /home/deploy/b2b/tradebridge-core/web && npm run lint && npm test && npm run build` → PASS
- [ ] End-to-end smoke (use the `verify` skill): buyer attaches image in chat → RFQ created with attachment → agent receives `rfq.matched` with attachments → (if ambiguous) clarification appears in thread → buyer replies with PDF → agent re-quotes. Deployment to switeaz.com happens via the `publish` skill (includes `make migrate` against prod DB and `ATTACHMENTS_DIR` env for both api and gateway services).
