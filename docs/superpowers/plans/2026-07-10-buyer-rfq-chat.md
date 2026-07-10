# Buyer RFQ Chat (ChatGPT-style) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reshape the buyer web into a ChatGPT-style chat that drives the existing extract→match→quote→order RFQ flow, backed by a new conversational-intake endpoint in Core.

**Architecture:** A stateless `POST /v1/rfq-chat` endpoint runs one intake step over the frontend-held conversation — a settings-driven LLM (with a deterministic heuristic fallback) decides *ask another question* vs *create the RFQ now*. On create it reuses the existing `rfq.Service.CreateAndMatch` pipeline and persists the transcript in a new nullable `rfqs.messages` column. The buyer web gains a `ChatShell` (sidebar of RFQ threads + app bar with a Core-reachability status dot) and `/chat` + `/chat/[rfqId]` routes; quotes render inline, polled every ~4s.

**Tech Stack:** Go 1.23 / Fiber / sqlc / pgx (Core); Next.js App Router / React 19 / TypeScript (web); vitest (one reducer unit test).

## Global Constraints

- New endpoint: `POST /v1/rfq-chat`, mounted under the **same tenant guard** as `/v1/rfqs` (`middleware.JWT` + `middleware.TenantGuard`). Request body: `{ "messages": [{ "role": "user"|"assistant", "content": string }] }`. Response: `{ "done": false, "assistant": string }` OR `{ "done": true, "assistant": string, "rfq": <rfqView>, "match_count": int }`.
- Intake must work with **zero API keys**: the heuristic path is deterministic and is the tested path. LLM intake mirrors `BuildExtractor`'s provider selection and **falls back to the heuristic on any API/parse error** — RFQ chat never fails because a provider is down.
- Clarification is capped: **at most 2 assistant questions** (`intakeMaxTurns = 2`), then the RFQ is created with whatever is present. An explicit "just create it" forces creation immediately.
- The transcript is persisted **only on RFQ creation**, in a new **nullable** `rfqs.messages JSONB` column (additive, reversible migration). Existing RFQs (null `messages`) must still load.
- Reuse the existing pipeline verbatim: the `create` branch calls `rfq.Service.CreateAndMatch` — no duplication of match/quote/event logic. Requirement extraction inside that call still uses the tenant's configured extractor.
- Frontend: buyers (`company_type` = `buyer`, `both`, or unset) land on `/chat` and use `ChatShell`; sellers-only and admins keep the existing `AppShell`. The status dot polls Core `/health` every ~10s (green "Connected" / red "Core offline"). Quotes poll `rfqQuotes` every ~4s while the RFQ isn't `closed`. Reuse the dark design tokens in `web/app/globals.css` (`var(--surface)`, `var(--border)`, `var(--muted)`, `var(--text)`, `var(--surface-2)`).
- `NEXT_PUBLIC_*` is baked at build time. The frontend gate is a clean `npm run build` + `npm run lint` (how the web is tested today), plus the Task 5 reducer unit test.

**Scope note — "tenant-guard rejects non-buyers" (spec §6):** the existing `/v1/rfqs` create route has **no** buyer-only role gate — `buyer`/`both`/unset companies are all buyers and there is no seller-only company that posts RFQs. To stay consistent with the rest of the RFQ surface, `/v1/rfq-chat` uses the **same guard as `/v1/rfqs`** (authenticated + tenant-scoped) and the test asserts an unauthenticated request is rejected (401), not a seller-role rejection. This is a deliberate, consistency-preserving reading of the spec wording; flag to the user if a hard buyer-only gate is actually wanted.

---

## File Structure

**Backend (Core):**
- `pkg/db/migrations/009_rfq_messages.up.sql` / `.down.sql` — add/drop nullable `rfqs.messages`.
- `pkg/db/query/rfq.sql` — add `SetRFQMessages` (:exec).
- `pkg/db/sqlc/*` — regenerated (adds `Rfq.Messages []byte`, `SetRFQMessages`).
- `internal/rfq/requirements.go` — add the `ChatMessage` domain type.
- `internal/rfq/intake.go` — `IntakeDecision`, `IntakeAgent`, `HeuristicIntake`, `BuildIntake`, question/derive helpers.
- `internal/rfq/intake_llm.go` — `llmChat` seam + `anthropicChat`/`openAIChat` adapters + `llmIntake`.
- `internal/rfq/service.go` — add `intakeFor` + `Chat` + `ChatResult`.
- `internal/rfq/handler.go` — add `chat` handler, register `POST /v1/rfq-chat`, `view()` includes `messages`.
- `test/integration/api_chat_test.go` — endpoint integration tests.
- `internal/rfq/intake_test.go` — heuristic + llm-with-fake unit tests (no DB).

**Frontend (web):**
- `web/lib/api.ts` — `ChatMessage` type, `rfqChat`, `messages?` on `RFQ`.
- `web/lib/chat.ts` — pure `chatReducer` + state/action types.
- `web/lib/chat.test.ts` — reducer unit test (vitest).
- `web/lib/health.ts` — `useCoreHealth` hook.
- `web/vitest.config.ts` + `package.json` — vitest devDep + `test` script.
- `web/components/ChatShell.tsx` — sidebar + app bar + status dot + bottom user menu.
- `web/app/(chat)/layout.tsx` — wraps chat routes in `ChatShell`.
- `web/app/(chat)/chat/page.tsx` — new-RFQ empty state + intake loop.
- `web/app/(chat)/chat/[rfqId]/page.tsx` — created thread + inline quotes + accept.
- `web/app/login/page.tsx`, `web/app/register/page.tsx`, `web/app/page.tsx`, `web/components/AppShell.tsx`, `web/app/(app)/dashboard/page.tsx` — route buyers to `/chat`.

---

## Task 1: `messages` column, `ChatMessage` type, `SetRFQMessages`, view wiring

**Files:**
- Create: `pkg/db/migrations/009_rfq_messages.up.sql`, `pkg/db/migrations/009_rfq_messages.down.sql`
- Modify: `pkg/db/query/rfq.sql` (append `SetRFQMessages`)
- Modify: `internal/rfq/requirements.go` (add `ChatMessage`)
- Modify: `internal/rfq/handler.go` (`rfqView.Messages` + `view()` decode)
- Regenerate: `pkg/db/sqlc/*` via `sqlc generate`
- Test: `test/integration/api_chat_test.go` (new file, `TestChat_MessagesRoundTrip`)

**Interfaces:**
- Produces: `rfq.ChatMessage{ Role, Content string }` (JSON `role`/`content`); sqlc `SetRFQMessages(ctx, SetRFQMessagesParams{ Messages []byte; ID string }) error`; `Rfq.Messages []byte`; `rfqView.Messages []ChatMessage` in the `GET /v1/rfqs/:id` and chat responses.

- [ ] **Step 1: Write the failing test**

Create `test/integration/api_chat_test.go`:

```go
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// registerBuyer registers a buyer company and returns (token, companyID, userID).
func registerBuyer(t *testing.T, api interface {
	Test(*http.Request, ...int) (*http.Response, error)
}, name string) (string, string) {
	t.Helper()
	st, s := doJSON(t, api.(interface{ Test(*http.Request, ...int) (*http.Response, error) }).(fiberApp), "POST", "/v1/auth/register", "", map[string]any{
		"company_name": name, "company_type": "buyer",
		"email": uniqueEmail("chat-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	return s["token"].(string), s["company_id"].(string)
}

// TestChat_MessagesRoundTrip proves the new column persists a transcript and
// GetRFQ returns it (proving migration 009 + SetRFQMessages + regen work).
func TestChat_MessagesRoundTrip(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "MsgBuyer", Type: "buyer"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: co.ID, Email: uniqueEmail("mrt-"), PasswordHash: "x", Role: "owner",
	})
	require.NoError(t, err)
	rfq, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: co.ID, CreatedBy: u.ID, Title: "Cartons", RawText: "need cartons",
	})
	require.NoError(t, err)

	transcript, err := json.Marshal([]map[string]string{
		{"role": "user", "content": "I need cartons"},
		{"role": "assistant", "content": "How many?"},
	})
	require.NoError(t, err)
	require.NoError(t, q.SetRFQMessages(ctx, sqlc.SetRFQMessagesParams{Messages: transcript, ID: rfq.ID}))

	got, err := q.GetRFQ(ctx, rfq.ID)
	require.NoError(t, err)
	require.JSONEq(t, string(transcript), string(got.Messages))
}
```

> NOTE: `CreateUser`/`CreateCompany` param field names are already defined in `pkg/db/sqlc`; if the helper `registerBuyer` above conflicts with existing helpers, drop it — only `TestChat_MessagesRoundTrip` is required for this task. `fiberApp` is not needed here; if it doesn't exist, remove the unused `registerBuyer` helper entirely and keep only the round-trip test.

- [ ] **Step 2: Run the test to verify it fails**

Run: `TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./test/integration/ -run TestChat_MessagesRoundTrip -v`
Expected: FAIL — `SetRFQMessages` / `Rfq.Messages` undefined (compile error).

- [ ] **Step 3: Add the migration**

`pkg/db/migrations/009_rfq_messages.up.sql`:

```sql
-- Persist the buyer↔assistant chat transcript that produced an RFQ. Nullable:
-- RFQs created via the classic /v1/rfqs form have no transcript.
ALTER TABLE rfqs ADD COLUMN messages JSONB;
```

`pkg/db/migrations/009_rfq_messages.down.sql`:

```sql
ALTER TABLE rfqs DROP COLUMN messages;
```

- [ ] **Step 4: Add the query**

Append to `pkg/db/query/rfq.sql`:

```sql
-- name: SetRFQMessages :exec
UPDATE rfqs
SET messages = sqlc.arg(messages)::jsonb, updated_at = NOW()
WHERE id = sqlc.arg(id);
```

- [ ] **Step 5: Add the `ChatMessage` type**

In `internal/rfq/requirements.go`, after the `Requirements` type:

```go
// ChatMessage is one turn of the buyer↔assistant RFQ-intake conversation. The
// frontend holds the running transcript and posts it to /v1/rfq-chat; on RFQ
// creation the whole transcript is persisted to rfqs.messages.
type ChatMessage struct {
	Role    string `json:"role"`    // "user" | "assistant"
	Content string `json:"content"`
}
```

- [ ] **Step 6: Regenerate sqlc, apply the migration**

Run:
```bash
sqlc generate
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up
```
Expected: `pkg/db/sqlc/models.go` gains `Messages []byte` on `Rfq`; `pkg/db/sqlc/rfq.sql.go` gains `SetRFQMessages`. Migration applies cleanly.

- [ ] **Step 7: Surface `messages` in the RFQ view**

In `internal/rfq/handler.go`, extend `rfqView` and `view()`:

```go
// rfqView is the JSON shape returned to buyers, with requirements decoded.
type rfqView struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	RawText      string        `json:"raw_text"`
	Requirements Requirements  `json:"requirements"`
	Category     string        `json:"category"`
	Status       string        `json:"status"`
	MatchCount   int32         `json:"match_count"`
	Messages     []ChatMessage `json:"messages,omitempty"`
	CreatedAt    string        `json:"created_at"`
}

func view(r sqlc.Rfq) rfqView {
	var req Requirements
	if len(r.Requirements) > 0 {
		_ = json.Unmarshal(r.Requirements, &req)
	}
	var msgs []ChatMessage
	if len(r.Messages) > 0 {
		_ = json.Unmarshal(r.Messages, &msgs)
	}
	return rfqView{
		ID:           r.ID,
		Title:        r.Title,
		RawText:      r.RawText,
		Requirements: req,
		Category:     r.Category,
		Status:       r.Status,
		MatchCount:   r.MatchCount,
		Messages:     msgs,
		CreatedAt:    r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}
```

- [ ] **Step 8: Run the test to verify it passes**

Run: `TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./test/integration/ -run TestChat_MessagesRoundTrip -v` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 9: Commit**

```bash
git add pkg/db/migrations/009_rfq_messages.*.sql pkg/db/query/rfq.sql pkg/db/sqlc internal/rfq/requirements.go internal/rfq/handler.go test/integration/api_chat_test.go
git commit -m "feat(rfq): add nullable rfqs.messages column + SetRFQMessages + view wiring"
```

---

## Task 2: Heuristic intake (deterministic ask/create decision)

**Files:**
- Create: `internal/rfq/intake.go`
- Create/Modify: `internal/rfq/intake_test.go` (heuristic cases)

**Interfaces:**
- Consumes: `HeuristicExtractor` (Task-existing), `ChatMessage` (Task 1).
- Produces:
  - `type IntakeDecision struct { Action string; Message, Title, Text string }` — `Action` is `"ask"` or `"create"`.
  - `type IntakeAgent interface { Decide(ctx context.Context, msgs []ChatMessage) (IntakeDecision, error) }`
  - `type HeuristicIntake struct{}` implementing `IntakeAgent`.
  - `func BuildIntake(cfg ProviderConfig, logger zerolog.Logger) IntakeAgent` — this task adds only the no-key branch (returns `HeuristicIntake{}`); Task 3 extends it.
  - `intakeMaxTurns = 2`.

- [ ] **Step 1: Write the failing tests**

Create `internal/rfq/intake_test.go`:

```go
package rfq

import (
	"context"
	"strings"
	"testing"
)

func TestHeuristicIntake_AsksForProductThenQuantityThenCreates(t *testing.T) {
	h := HeuristicIntake{}
	ctx := context.Background()

	// Turn 1: vague opener with no product/qty → ask for product.
	d, err := h.Decide(ctx, []ChatMessage{{Role: "user", Content: "hi I want to buy something"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "ask" {
		t.Fatalf("turn1 action = %q, want ask", d.Action)
	}
	if d.Message == "" {
		t.Fatal("turn1 expected a clarifying question")
	}

	// Turn 2: product now known, quantity still missing → ask for quantity.
	d, err = h.Decide(ctx, []ChatMessage{
		{Role: "user", Content: "hi I want to buy something"},
		{Role: "assistant", Content: d.Message},
		{Role: "user", Content: "cartons for packaging"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "ask" {
		t.Fatalf("turn2 action = %q, want ask", d.Action)
	}

	// Turn 3: product + quantity present → create.
	d, err = h.Decide(ctx, []ChatMessage{
		{Role: "user", Content: "hi I want to buy something"},
		{Role: "assistant", Content: "q1"},
		{Role: "user", Content: "cartons for packaging"},
		{Role: "assistant", Content: "q2"},
		{Role: "user", Content: "500 cartons delivered to Dubai"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "create" {
		t.Fatalf("turn3 action = %q, want create", d.Action)
	}
	if d.Title == "" || d.Text == "" {
		t.Fatalf("create decision needs title+text, got title=%q text=%q", d.Title, d.Text)
	}
	if !strings.Contains(d.Text, "cartons") {
		t.Fatalf("text should carry the conversation, got %q", d.Text)
	}
}

func TestHeuristicIntake_ExplicitCreateShortCircuits(t *testing.T) {
	d, err := HeuristicIntake{}.Decide(context.Background(), []ChatMessage{
		{Role: "user", Content: "widgets"},
		{Role: "assistant", Content: "how many?"},
		{Role: "user", Content: "just create it"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "create" {
		t.Fatalf("action = %q, want create on explicit request", d.Action)
	}
}

func TestHeuristicIntake_CapsAtMaxTurns(t *testing.T) {
	// Two assistant questions already asked; still nothing extractable → create anyway.
	d, err := HeuristicIntake{}.Decide(context.Background(), []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "q1"},
		{Role: "user", Content: "still thinking"},
		{Role: "assistant", Content: "q2"},
		{Role: "user", Content: "maybe some stuff"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "create" {
		t.Fatalf("action = %q, want create at turn cap", d.Action)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/rfq/ -run TestHeuristicIntake -v`
Expected: FAIL — `HeuristicIntake` undefined.

- [ ] **Step 3: Implement `internal/rfq/intake.go`**

```go
package rfq

import (
	"context"
	"regexp"
	"strings"

	"github.com/rs/zerolog"
)

// intakeMaxTurns caps how many clarifying questions the assistant asks before
// creating the RFQ with whatever information is present.
const intakeMaxTurns = 2

// IntakeDecision is one step of RFQ intake: either ask another question or
// create the RFQ now. On create, Title/Text feed the existing CreateAndMatch.
type IntakeDecision struct {
	Action  string // "ask" | "create"
	Message string // the clarifying question (when Action == "ask")
	Title   string // RFQ title (when Action == "create")
	Text    string // RFQ raw text (when Action == "create")
}

// IntakeAgent turns the running conversation into the next IntakeDecision.
type IntakeAgent interface {
	Decide(ctx context.Context, msgs []ChatMessage) (IntakeDecision, error)
}

// BuildIntake picks an intake agent from a resolved config, mirroring
// BuildExtractor. With no API key it uses the deterministic heuristic, which
// keeps RFQ chat fully functional with zero external dependencies.
func BuildIntake(cfg ProviderConfig, logger zerolog.Logger) IntakeAgent {
	switch {
	case cfg.APIKey == "":
		return HeuristicIntake{}
	case cfg.Provider == "openai_compatible":
		return llmIntake{chat: newOpenAIChat(cfg.BaseURL, cfg.Model, cfg.APIKey, logger), fallback: HeuristicIntake{}, logger: logger}
	default: // anthropic
		model := cfg.Model
		if model == "" {
			model = "claude-haiku-4-5"
		}
		return llmIntake{chat: newAnthropicChat(cfg.APIKey, model, logger), fallback: HeuristicIntake{}, logger: logger}
	}
}

// HeuristicIntake decides ask-vs-create with no network calls, reusing the
// heuristic extractor to detect the product/category and quantity slots.
type HeuristicIntake struct{}

var reExplicitCreate = regexp.MustCompile(`(?i)\b(just create|go ahead|create it|submit it|post it|that'?s all|that is all|no more)\b`)

const (
	askProduct  = "What product or material are you looking to source?"
	askQuantity = "About how many do you need, and where should it be delivered?"
)

// Decide implements IntakeAgent.
func (HeuristicIntake) Decide(ctx context.Context, msgs []ChatMessage) (IntakeDecision, error) {
	userText := joinUser(msgs)
	asked := countAssistant(msgs)

	if last := lastUser(msgs); reExplicitCreate.MatchString(last) {
		return createDecision(userText), nil
	}

	req, _ := HeuristicExtractor{}.Extract(ctx, "", userText)
	hasProduct := req.Category != "" || len(req.Keywords) > 0
	hasQty := req.Quantity != ""

	switch {
	case hasProduct && hasQty:
		return createDecision(userText), nil
	case asked >= intakeMaxTurns:
		return createDecision(userText), nil
	case !hasProduct:
		return IntakeDecision{Action: "ask", Message: askProduct}, nil
	default: // has product, missing quantity
		return IntakeDecision{Action: "ask", Message: askQuantity}, nil
	}
}

// createDecision builds a create decision from the accumulated user text.
func createDecision(userText string) IntakeDecision {
	return IntakeDecision{Action: "create", Title: deriveTitle(userText), Text: userText}
}

// deriveTitle makes a short RFQ title from the conversation (first line, capped),
// falling back to a generic label so CreateAndMatch never sees an empty title.
func deriveTitle(userText string) string {
	line := userText
	if i := strings.IndexAny(line, ".\n"); i > 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "RFQ"
	}
	const max = 60
	if len(line) > max {
		line = strings.TrimSpace(line[:max])
	}
	return line
}

func joinUser(msgs []ChatMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == "user" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(m.Content)
		}
	}
	return strings.TrimSpace(b.String())
}

func countAssistant(msgs []ChatMessage) int {
	n := 0
	for _, m := range msgs {
		if m.Role == "assistant" {
			n++
		}
	}
	return n
}

func lastUser(msgs []ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}
```

> NOTE: `intake.go` references `newAnthropicChat`/`newOpenAIChat`/`llmIntake` which Task 3 creates. Implement Task 2 and Task 3 together if `go build` must pass mid-task; the heuristic tests here do not exercise those symbols but the package won't compile without them. If you run Task 2 in isolation, temporarily make `BuildIntake` return `HeuristicIntake{}` for all non-empty-key branches, then restore in Task 3.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/rfq/ -run TestHeuristicIntake -v`
Expected: PASS (with the Task 3 symbols present, or the temporary `BuildIntake` stub).

- [ ] **Step 5: Commit**

```bash
git add internal/rfq/intake.go internal/rfq/intake_test.go
git commit -m "feat(rfq): heuristic RFQ-chat intake (deterministic ask/create)"
```

---

## Task 3: LLM intake (settings-driven, with heuristic fallback)

**Files:**
- Create: `internal/rfq/intake_llm.go`
- Modify: `internal/rfq/intake_test.go` (fake-chat cases)

**Interfaces:**
- Consumes: `ChatMessage`, `IntakeDecision`, `IntakeAgent`, `HeuristicIntake` (Task 2); `chatMessage`/`chatRequest`/`chatResponse` (existing in `openai.go`); anthropic SDK (existing in `claude.go`).
- Produces:
  - `type llmChat interface { complete(ctx context.Context, system string, msgs []ChatMessage) (string, error) }`
  - `func newAnthropicChat(apiKey, model string, logger zerolog.Logger) llmChat`
  - `func newOpenAIChat(baseURL, model, apiKey string, logger zerolog.Logger) llmChat`
  - `type llmIntake struct { chat llmChat; fallback IntakeAgent; logger zerolog.Logger }` implementing `IntakeAgent`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/rfq/intake_test.go`:

```go
type fakeChat struct {
	reply string
	err   error
}

func (f fakeChat) complete(_ context.Context, _ string, _ []ChatMessage) (string, error) {
	return f.reply, f.err
}

func newTestLLMIntake(reply string, err error) llmIntake {
	return llmIntake{chat: fakeChat{reply: reply, err: err}, fallback: HeuristicIntake{}, logger: zerolog.Nop()}
}

func TestLLMIntake_ParsesAsk(t *testing.T) {
	d, err := newTestLLMIntake(`{"action":"ask","message":"What's the target price?"}`, nil).
		Decide(context.Background(), []ChatMessage{{Role: "user", Content: "cartons"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "ask" || d.Message != "What's the target price?" {
		t.Fatalf("got %+v", d)
	}
}

func TestLLMIntake_ParsesCreate(t *testing.T) {
	d, err := newTestLLMIntake(`{"action":"create","title":"Cartons","rfq_text":"500 cartons to Dubai"}`, nil).
		Decide(context.Background(), []ChatMessage{{Role: "user", Content: "500 cartons to Dubai"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "create" || d.Title != "Cartons" || d.Text != "500 cartons to Dubai" {
		t.Fatalf("got %+v", d)
	}
}

func TestLLMIntake_FallsBackOnError(t *testing.T) {
	// Provider error → heuristic decides (product+qty present → create).
	d, err := newTestLLMIntake("", context.DeadlineExceeded).
		Decide(context.Background(), []ChatMessage{{Role: "user", Content: "500 cartons for packaging to Dubai"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "create" {
		t.Fatalf("expected heuristic create on fallback, got %+v", d)
	}
}

func TestLLMIntake_FallsBackOnGarbage(t *testing.T) {
	d, err := newTestLLMIntake("not json at all", nil).
		Decide(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "ask" { // heuristic asks for product on a vague opener
		t.Fatalf("expected heuristic ask on garbage, got %+v", d)
	}
}
```

Add `"github.com/rs/zerolog"` to the test imports.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/rfq/ -run TestLLMIntake -v`
Expected: FAIL — `llmIntake`/`fakeChat` wiring undefined.

- [ ] **Step 3: Implement `internal/rfq/intake_llm.go`**

```go
package rfq

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/rs/zerolog"
)

// intakeSystemPrompt tells the model to return exactly one JSON decision. The
// service composes the user-facing confirmation on create, so on "create" the
// model only needs to supply a title + rfq_text.
const intakeSystemPrompt = `You are a procurement assistant helping a buyer post an RFQ (request for quote).
Given the conversation so far, decide whether you have enough to post the RFQ or need ONE more clarifying question.
Respond with ONLY a JSON object, no prose, matching exactly one of:
{"action":"ask","message":"<a single short clarifying question>"}
{"action":"create","title":"<short RFQ title>","rfq_text":"<one paragraph capturing what the buyer needs>"}
Prefer "create" once you know the product/material and a rough quantity. Ask at most one more question. If the buyer says to just post it, use "create".`

// llmDecision is the JSON the model returns.
type llmDecision struct {
	Action  string `json:"action"`
	Message string `json:"message"`
	Title   string `json:"title"`
	RFQText string `json:"rfq_text"`
}

// llmChat is a minimal chat-completion seam over a system prompt + the running
// conversation, returning the model's raw text.
type llmChat interface {
	complete(ctx context.Context, system string, msgs []ChatMessage) (string, error)
}

// llmIntake decides via the model, degrading to the heuristic on any API or
// parse error so RFQ chat never fails because a provider is unavailable.
type llmIntake struct {
	chat     llmChat
	fallback IntakeAgent
	logger   zerolog.Logger
}

func (l llmIntake) Decide(ctx context.Context, msgs []ChatMessage) (IntakeDecision, error) {
	raw, err := l.chat.complete(ctx, intakeSystemPrompt, msgs)
	if err != nil {
		l.logger.Warn().Err(err).Msg("intake LLM call failed; using heuristic")
		return l.fallback.Decide(ctx, msgs)
	}
	dec, ok := parseDecision(raw)
	if !ok {
		l.logger.Warn().Str("raw", raw).Msg("intake LLM returned unparseable JSON; using heuristic")
		return l.fallback.Decide(ctx, msgs)
	}
	if dec.Action == "create" {
		title := strings.TrimSpace(dec.Title)
		text := strings.TrimSpace(dec.RFQText)
		if text == "" {
			text = joinUser(msgs)
		}
		if title == "" {
			title = deriveTitle(text)
		}
		return IntakeDecision{Action: "create", Title: title, Text: text}, nil
	}
	// Any non-create decision is treated as ask; empty question → heuristic.
	q := strings.TrimSpace(dec.Message)
	if q == "" {
		return l.fallback.Decide(ctx, msgs)
	}
	return IntakeDecision{Action: "ask", Message: q}, nil
}

// parseDecision pulls the first JSON object out of the model reply.
func parseDecision(s string) (llmDecision, bool) {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return llmDecision{}, false
	}
	var d llmDecision
	if err := json.Unmarshal([]byte(s[start:end+1]), &d); err != nil {
		return llmDecision{}, false
	}
	if d.Action != "ask" && d.Action != "create" {
		return llmDecision{}, false
	}
	return d, true
}

// --- anthropic adapter ---

type anthropicChat struct {
	client anthropic.Client
	model  anthropic.Model
}

func newAnthropicChat(apiKey, model string, _ zerolog.Logger) llmChat {
	return anthropicChat{client: anthropic.NewClient(option.WithAPIKey(apiKey)), model: anthropic.Model(model)}
}

func (a anthropicChat) complete(ctx context.Context, system string, msgs []ChatMessage) (string, error) {
	params := anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: 512,
		System:    []anthropic.TextBlockParam{{Text: system}},
	}
	for _, m := range msgs {
		if m.Role == "assistant" {
			params.Messages = append(params.Messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		} else {
			params.Messages = append(params.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		}
	}
	// Anthropic requires the first message to be a user turn.
	if len(params.Messages) == 0 {
		params.Messages = append(params.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock("")))
	}
	resp, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			b.WriteString(tb.Text)
		}
	}
	return b.String(), nil
}

// --- openai-compatible adapter (reuses chatMessage/chatRequest/chatResponse from openai.go) ---

type openAIChat struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func newOpenAIChat(baseURL, model, apiKey string, _ zerolog.Logger) llmChat {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	return openAIChat{baseURL: baseURL, model: model, apiKey: apiKey, client: &http.Client{Timeout: 30 * time.Second}}
}

func (o openAIChat) complete(ctx context.Context, system string, msgs []ChatMessage) (string, error) {
	payload := chatRequest{Model: o.model, Messages: []chatMessage{{Role: "system", Content: system}}}
	for _, m := range msgs {
		role := "user"
		if m.Role == "assistant" {
			role = "assistant"
		}
		payload.Messages = append(payload.Messages, chatMessage{Role: role, Content: m.Content})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	resp, err := o.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", &httpStatusError{code: resp.StatusCode}
	}
	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.Choices) == 0 {
		return "", err
	}
	return out.Choices[0].Message.Content, nil
}

// httpStatusError carries a non-200 status so llmIntake falls back to heuristic.
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return "openai chat non-200 status" }
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/rfq/ -run 'TestLLMIntake|TestHeuristicIntake' -v && go build ./...`
Expected: PASS; build clean. (If Task 2 used the temporary `BuildIntake` stub, restore the real branches now.)

- [ ] **Step 5: Commit**

```bash
git add internal/rfq/intake_llm.go internal/rfq/intake_test.go internal/rfq/intake.go
git commit -m "feat(rfq): settings-driven LLM RFQ-chat intake with heuristic fallback"
```

---

## Task 4: `Chat` service method + `POST /v1/rfq-chat` endpoint

**Files:**
- Modify: `internal/rfq/service.go` (`intakeFor`, `ChatResult`, `Chat`)
- Modify: `internal/rfq/handler.go` (`chat` handler + route)
- Modify: `test/integration/api_chat_test.go` (endpoint tests)

**Interfaces:**
- Consumes: `IntakeAgent`/`BuildIntake` (Tasks 2–3), `ExtractorResolver` + `Service.resolver` (existing), `Service.CreateAndMatch` (existing), `SetRFQMessages` (Task 1).
- Produces:
  - `type ChatResult struct { Done bool; Assistant string; RFQ *sqlc.Rfq; MatchCount int }`
  - `func (s *Service) Chat(ctx context.Context, buyerCompanyID, userID string, msgs []ChatMessage) (ChatResult, error)`
  - Route `POST /v1/rfq-chat` returning the Global-Constraints response shape.

- [ ] **Step 1: Write the failing endpoint tests**

Append to `test/integration/api_chat_test.go`:

```go
// TestChat_HeuristicAsksThenCreates walks the no-key heuristic path end to end.
func TestChat_HeuristicAsksThenCreates(t *testing.T) {
	api := buildAPI(t)

	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "ChatBuyer", "company_type": "buyer",
		"email": uniqueEmail("cb-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	token := buyer["token"].(string)

	// Vague opener → assistant asks (done=false, no rfq).
	st, r1 := doJSON(t, api, "POST", "/v1/rfq-chat", token, map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hi, I want to buy something"}},
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, false, r1["done"])
	require.NotEmpty(t, r1["assistant"])
	require.Nil(t, r1["rfq"])

	// Full detail → assistant creates (done=true, rfq present, transcript persisted).
	st, r2 := doJSON(t, api, "POST", "/v1/rfq-chat", token, map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": "hi, I want to buy something"},
			{"role": "assistant", "content": r1["assistant"].(string)},
			{"role": "user", "content": "500 cartons for packaging, deliver to Dubai next month"},
		},
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, true, r2["done"])
	rfq := r2["rfq"].(map[string]any)
	rfqID := rfq["id"].(string)
	require.NotEmpty(t, rfqID)
	msgs := rfq["messages"].([]any)
	require.GreaterOrEqual(t, len(msgs), 3, "transcript persisted incl. assistant confirmation")

	// GET returns the persisted transcript.
	st, got := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID, token, nil)
	require.Equal(t, http.StatusOK, st)
	require.NotEmpty(t, got["rfq"].(map[string]any)["messages"])
}

// TestChat_RequiresAuth proves the endpoint is behind the tenant guard.
func TestChat_RequiresAuth(t *testing.T) {
	api := buildAPI(t)
	st, _ := doJSON(t, api, "POST", "/v1/rfq-chat", "", map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "x"}},
	})
	require.Equal(t, http.StatusUnauthorized, st)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./test/integration/ -run TestChat_ -v`
Expected: FAIL — `/v1/rfq-chat` returns 404 (route not registered).

- [ ] **Step 3: Add `intakeFor`, `ChatResult`, `Chat` to `internal/rfq/service.go`**

Add imports as needed (`errors` not required). Insert after `extractorFor`:

```go
// intakeFor resolves the buyer's provider config and builds an intake agent,
// degrading to the heuristic on resolver error (same policy as extractorFor).
func (s *Service) intakeFor(ctx context.Context, companyID string) IntakeAgent {
	cfg, err := s.resolver.ExtractionConfig(ctx, companyID)
	if err != nil {
		s.logger.Warn().Err(err).Str("company_id", companyID).Msg("intake config resolve failed; using heuristic")
		return HeuristicIntake{}
	}
	return BuildIntake(cfg, s.logger)
}

// ChatResult is one RFQ-chat step: either a clarifying question (Done=false) or
// the created RFQ (Done=true, RFQ populated).
type ChatResult struct {
	Done       bool
	Assistant  string
	RFQ        *sqlc.Rfq
	MatchCount int
}

// Chat runs one intake step. On "create" it reuses CreateAndMatch and persists
// the full transcript (incl. the confirmation) to rfqs.messages.
func (s *Service) Chat(ctx context.Context, buyerCompanyID, userID string, msgs []ChatMessage) (ChatResult, error) {
	dec, err := s.intakeFor(ctx, buyerCompanyID).Decide(ctx, msgs)
	if err != nil {
		return ChatResult{}, err
	}
	if dec.Action != "create" {
		return ChatResult{Done: false, Assistant: dec.Message}, nil
	}

	opened, matches, err := s.CreateAndMatch(ctx, buyerCompanyID, userID, dec.Title, dec.Text)
	if err != nil {
		return ChatResult{}, err
	}

	confirm := chatConfirmation(opened.Title, len(matches))
	transcript := append(append([]ChatMessage{}, msgs...), ChatMessage{Role: "assistant", Content: confirm})
	raw, err := json.Marshal(transcript)
	if err != nil {
		return ChatResult{}, err
	}
	if err := s.q.SetRFQMessages(ctx, sqlc.SetRFQMessagesParams{Messages: raw, ID: opened.ID}); err != nil {
		return ChatResult{}, err
	}
	opened.Messages = raw
	return ChatResult{Done: true, Assistant: confirm, RFQ: &opened, MatchCount: len(matches)}, nil
}

// chatConfirmation is the assistant's closing line after posting an RFQ.
func chatConfirmation(title string, n int) string {
	plural := "s"
	if n == 1 {
		plural = ""
	}
	return fmt.Sprintf("Done — I've posted your RFQ %q and matched %d seller%s. Quotes will appear here as sellers respond.", title, n, plural)
}
```

Add `"fmt"` to the `service.go` import block.

- [ ] **Step 4: Add the `chat` handler + route in `internal/rfq/handler.go`**

In `Register`, add the route (a sibling group so it isn't nested under `/v1/rfqs`):

```go
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/rfqs", guard...)
	g.Post("/", h.create)
	g.Get("/", h.list)
	g.Get("/:id", h.get)
	g.Post("/:id/close", h.close)

	app.Post("/v1/rfq-chat", append(append([]fiber.Handler{}, guard...), h.chat)...)
}
```

Add the handler + request type:

```go
type chatReq struct {
	Messages []ChatMessage `json:"messages"`
}

func (h *Handler) chat(c *fiber.Ctx) error {
	var req chatReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if len(req.Messages) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "messages required")
	}
	res, err := h.svc.Chat(c.Context(), middleware.CompanyID(c), middleware.UserID(c), req.Messages)
	if err != nil {
		return err
	}
	out := fiber.Map{"done": res.Done, "assistant": res.Assistant}
	if res.Done && res.RFQ != nil {
		out["rfq"] = view(*res.RFQ)
		out["match_count"] = res.MatchCount
	}
	return c.JSON(out)
}
```

- [ ] **Step 5: Run to verify pass**

Run: `TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./test/integration/ -run TestChat_ -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 6: Full backend gate**

Run: `go build ./... && go vet ./... && TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./...`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/rfq/service.go internal/rfq/handler.go test/integration/api_chat_test.go
git commit -m "feat(rfq): POST /v1/rfq-chat conversational intake endpoint"
```

---

## Task 5: Frontend API client + pure chat reducer (+ vitest)

**Files:**
- Modify: `web/lib/api.ts` (`ChatMessage`, `rfqChat`, `RFQ.messages`)
- Create: `web/lib/chat.ts` (reducer)
- Create: `web/lib/chat.test.ts` (vitest)
- Create: `web/vitest.config.ts`
- Modify: `web/package.json` (vitest devDep + `test` script)

**Interfaces:**
- Produces:
  - `type ChatMessage = { role: "user" | "assistant"; content: string }`
  - `rfqChat(token, messages): Promise<{ done: boolean; assistant: string; rfq?: RFQ; match_count?: number }>`
  - `RFQ.messages?: ChatMessage[]`
  - `chatReducer(state, action)`, `initialChatState`, `ChatState`, `ChatAction`.

- [ ] **Step 1: Add types + client call in `web/lib/api.ts`**

Add to the types section (near `RFQ`):

```ts
export type ChatMessage = { role: "user" | "assistant"; content: string };
```

Add `messages?: ChatMessage[];` to the `RFQ` type (after `match_count`). Add to the buyer RFQ section:

```ts
export const rfqChat = (token: string, messages: ChatMessage[]) =>
  req<{ done: boolean; assistant: string; rfq?: RFQ; match_count?: number }>(
    "POST",
    "/v1/rfq-chat",
    { token, body: { messages } },
  );
```

- [ ] **Step 2: Write the failing reducer test**

Create `web/lib/chat.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { chatReducer, initialChatState } from "./chat";

describe("chatReducer", () => {
  it("send appends a user message and marks pending", () => {
    const s = chatReducer(initialChatState, { type: "send", content: "I need cartons" });
    expect(s.messages).toEqual([{ role: "user", content: "I need cartons" }]);
    expect(s.pending).toBe(true);
    expect(s.error).toBeNull();
  });

  it("reply appends an assistant message and clears pending", () => {
    const sent = chatReducer(initialChatState, { type: "send", content: "hi" });
    const s = chatReducer(sent, { type: "reply", assistant: "What product?" });
    expect(s.messages.at(-1)).toEqual({ role: "assistant", content: "What product?" });
    expect(s.pending).toBe(false);
  });

  it("reply with rfqId records it (RFQ created)", () => {
    const s = chatReducer(initialChatState, { type: "reply", assistant: "Done", rfqId: "rfq_1" });
    expect(s.rfqId).toBe("rfq_1");
  });

  it("error clears pending and preserves messages", () => {
    const sent = chatReducer(initialChatState, { type: "send", content: "hi" });
    const s = chatReducer(sent, { type: "error", error: "Couldn't reach Core" });
    expect(s.pending).toBe(false);
    expect(s.error).toBe("Couldn't reach Core");
    expect(s.messages).toHaveLength(1);
  });
});
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd web && npm install -D vitest && npx vitest run lib/chat.test.ts`
Expected: FAIL — `./chat` module not found.

> If installing vitest is not possible in the execution environment, still create `web/lib/chat.ts` as a pure module (Step 4); the hard gate for this task then falls back to `npm run build && npm run lint`, and the reducer test is added once a runner is available. Do not skip creating the pure reducer — later tasks import it.

- [ ] **Step 4: Implement `web/lib/chat.ts`**

```ts
import type { ChatMessage } from "./api";

export type ChatState = {
  messages: ChatMessage[];
  pending: boolean;
  error: string | null;
  rfqId: string | null;
};

export type ChatAction =
  | { type: "send"; content: string }
  | { type: "reply"; assistant: string; rfqId?: string }
  | { type: "error"; error: string }
  | { type: "reset" };

export const initialChatState: ChatState = {
  messages: [],
  pending: false,
  error: null,
  rfqId: null,
};

export function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case "send":
      return {
        ...state,
        messages: [...state.messages, { role: "user", content: action.content }],
        pending: true,
        error: null,
      };
    case "reply":
      return {
        ...state,
        messages: [...state.messages, { role: "assistant", content: action.assistant }],
        pending: false,
        error: null,
        rfqId: action.rfqId ?? state.rfqId,
      };
    case "error":
      return { ...state, pending: false, error: action.error };
    case "reset":
      return initialChatState;
    default:
      return state;
  }
}
```

- [ ] **Step 5: Add vitest config + script**

Create `web/vitest.config.ts`:

```ts
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: { environment: "node", include: ["lib/**/*.test.ts"] },
});
```

Add to `web/package.json` `scripts`: `"test": "vitest run"`.

- [ ] **Step 6: Run to verify pass + build stays clean**

Run: `cd web && npx vitest run && npm run lint && npm run build`
Expected: reducer test PASS; lint + build clean.

- [ ] **Step 7: Commit**

```bash
git add web/lib/api.ts web/lib/chat.ts web/lib/chat.test.ts web/vitest.config.ts web/package.json web/package-lock.json
git commit -m "feat(web): rfqChat client + pure chat reducer with vitest"
```

---

## Task 6: `ChatShell` + Core-health status hook

**Files:**
- Create: `web/lib/health.ts` (`useCoreHealth`)
- Create: `web/components/ChatShell.tsx`

**Interfaces:**
- Consumes: `useMe` (`web/lib/auth.ts`), `listRfqs`/`health`/`Me`/`RFQ` (`web/lib/api.ts`), `logout`.
- Produces:
  - `useCoreHealth(intervalMs?: number): boolean | null` (null = unknown, true = reachable, false = offline).
  - `default export ChatShell({ children })` — sidebar + app bar + main, buyer auth-guarded like `AppShell`.

- [ ] **Step 1: Implement `web/lib/health.ts`**

```ts
"use client";

import { useEffect, useState } from "react";
import { health } from "./api";

// useCoreHealth polls Core /health so the app bar can show reachability.
// Returns null until the first probe settles, then true/false.
export function useCoreHealth(intervalMs = 10000): boolean | null {
  const [ok, setOk] = useState<boolean | null>(null);
  useEffect(() => {
    let active = true;
    const probe = () =>
      health()
        .then(() => active && setOk(true))
        .catch(() => active && setOk(false));
    probe();
    const t = setInterval(probe, intervalMs);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, [intervalMs]);
  return ok;
}
```

- [ ] **Step 2: Implement `web/components/ChatShell.tsx`**

```tsx
"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { logout, useMe } from "@/lib/auth";
import { listRfqs, type Me, type RFQ } from "@/lib/api";
import { useCoreHealth } from "@/lib/health";

const isSeller = (m: Me) => m.company_type === "seller" || m.company_type === "both";
const isAdmin = (m: Me) => m.role === "owner" || m.role === "admin";

export default function ChatShell({ children }: { children: React.ReactNode }) {
  const { token, me, ready } = useMe();
  const router = useRouter();
  const pathname = usePathname();
  const [hydrated, setHydrated] = useState(false);
  const [rfqs, setRfqs] = useState<RFQ[]>([]);
  const online = useCoreHealth();

  useEffect(() => setHydrated(true), []);
  useEffect(() => {
    if (hydrated && ready && !token) router.replace("/login");
  }, [hydrated, ready, token, router]);

  // Refresh the thread list on navigation (a new thread appears after create).
  useEffect(() => {
    if (!token) return;
    listRfqs(token).then((r) => setRfqs(r.rfqs)).catch(() => {});
  }, [token, pathname]);

  if (!hydrated || !ready) return <Centered>Loading…</Centered>;
  if (!me) return <Centered>Redirecting…</Centered>;

  const activeId = pathname.startsWith("/chat/") ? pathname.slice("/chat/".length) : null;
  const title =
    activeId ? rfqs.find((r) => r.id === activeId)?.title ?? "RFQ" : "New RFQ";

  return (
    <div style={{ display: "flex", minHeight: "100vh" }}>
      <aside
        style={{
          width: 264,
          flexShrink: 0,
          borderRight: "1px solid var(--border)",
          background: "var(--surface)",
          display: "flex",
          flexDirection: "column",
        }}
      >
        <div style={{ padding: "0.9rem" }}>
          <Link
            href="/chat"
            className="btn btn-primary"
            style={{ display: "block", textAlign: "center", width: "100%" }}
          >
            ＋ New RFQ
          </Link>
        </div>
        <nav style={{ flex: 1, overflowY: "auto", padding: "0 0.6rem", display: "flex", flexDirection: "column", gap: "0.2rem" }}>
          {rfqs.map((r) => {
            const active = r.id === activeId;
            return (
              <Link
                key={r.id}
                href={`/chat/${r.id}`}
                style={{
                  display: "block",
                  padding: "0.5rem 0.6rem",
                  borderRadius: 7,
                  fontSize: "0.88rem",
                  color: active ? "var(--text)" : "var(--muted)",
                  background: active ? "var(--surface-2)" : "transparent",
                  whiteSpace: "nowrap",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                }}
              >
                {r.title || "Untitled RFQ"}
              </Link>
            );
          })}
          {rfqs.length === 0 && (
            <p className="muted small" style={{ padding: "0.5rem 0.6rem" }}>
              No RFQs yet — start one above.
            </p>
          )}
        </nav>
        <div style={{ borderTop: "1px solid var(--border)", padding: "0.7rem 0.9rem", display: "flex", flexDirection: "column", gap: "0.35rem" }}>
          <strong style={{ fontSize: "0.9rem", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
            {me.company_name ?? "—"}
          </strong>
          {isSeller(me) && <Link href="/seller/profile" className="small muted">Seller console</Link>}
          {isAdmin(me) && <Link href="/admin/models" className="small muted">Admin settings</Link>}
          <button
            className="btn btn-sm btn-ghost"
            style={{ marginTop: "0.25rem", alignSelf: "flex-start" }}
            onClick={() => {
              logout();
              router.replace("/login");
            }}
          >
            Sign out
          </button>
        </div>
      </aside>

      <div style={{ flex: 1, display: "flex", flexDirection: "column", minWidth: 0 }}>
        <header
          style={{
            height: 56,
            borderBottom: "1px solid var(--border)",
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            padding: "0 1.5rem",
            gap: "1rem",
          }}
        >
          <strong style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{title}</strong>
          <div className="row" style={{ gap: "0.6rem", alignItems: "center" }}>
            <StatusDot online={online} />
            <span className="badge badge-info">{me.company_type ?? "buyer"}</span>
          </div>
        </header>
        <main style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column" }}>{children}</main>
      </div>
    </div>
  );
}

function StatusDot({ online }: { online: boolean | null }) {
  const color = online == null ? "var(--muted)" : online ? "#22c55e" : "#ef4444";
  const label = online == null ? "Checking…" : online ? "Connected" : "Core offline";
  return (
    <span className="row small muted" style={{ gap: "0.4rem", alignItems: "center" }}>
      <span style={{ width: 8, height: 8, borderRadius: "50%", background: color, display: "inline-block" }} />
      {label}
    </span>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center" }} className="muted">
      {children}
    </div>
  );
}
```

- [ ] **Step 3: Verify build + lint**

Run: `cd web && npm run lint && npm run build`
Expected: clean. (ChatShell is not yet routed; this task's gate is compile + lint.)

- [ ] **Step 4: Commit**

```bash
git add web/lib/health.ts web/components/ChatShell.tsx
git commit -m "feat(web): ChatShell (RFQ thread sidebar + app bar + Core-health dot)"
```

---

## Task 7: `/chat` route — new-RFQ intake loop

**Files:**
- Create: `web/app/(chat)/layout.tsx`
- Create: `web/app/(chat)/chat/page.tsx`

**Interfaces:**
- Consumes: `ChatShell` (Task 6), `chatReducer`/`initialChatState` (Task 5), `rfqChat` (Task 5), `useToken`.
- Produces: the `/chat` empty-state + intake conversation; on `done` it routes to `/chat/<rfqId>`.

- [ ] **Step 1: Route group layout `web/app/(chat)/layout.tsx`**

```tsx
import ChatShell from "@/components/ChatShell";

export default function ChatLayout({ children }: { children: React.ReactNode }) {
  return <ChatShell>{children}</ChatShell>;
}
```

- [ ] **Step 2: `web/app/(chat)/chat/page.tsx`**

```tsx
"use client";

import { useReducer, useState } from "react";
import { useRouter } from "next/navigation";
import { useToken } from "@/lib/auth";
import { rfqChat } from "@/lib/api";
import { chatReducer, initialChatState } from "@/lib/chat";

export default function NewChat() {
  const token = useToken();
  const router = useRouter();
  const [state, dispatch] = useReducer(chatReducer, initialChatState);
  const [draft, setDraft] = useState("");

  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    const content = draft.trim();
    if (!content || !token || state.pending) return;
    const next = [...state.messages, { role: "user" as const, content }];
    dispatch({ type: "send", content });
    setDraft("");
    try {
      const res = await rfqChat(token, next);
      if (res.done && res.rfq) {
        router.push(`/chat/${res.rfq.id}`);
        return;
      }
      dispatch({ type: "reply", assistant: res.assistant });
    } catch (err) {
      dispatch({ type: "error", error: err instanceof Error ? err.message : "Couldn't reach Core — retry" });
    }
  };

  const empty = state.messages.length === 0;

  return (
    <div style={{ flex: 1, display: "flex", flexDirection: "column", minWidth: 0 }}>
      <div style={{ flex: 1, overflowY: "auto", padding: "1.5rem", maxWidth: 760, width: "100%", margin: "0 auto" }}>
        {empty ? (
          <div style={{ textAlign: "center", marginTop: "18vh" }}>
            <h1 style={{ fontSize: "1.5rem" }}>What do you need to source?</h1>
            <p className="muted" style={{ marginTop: "0.5rem" }}>
              Describe it in plain language — I&apos;ll match sellers and collect quotes.
            </p>
          </div>
        ) : (
          <div className="col" style={{ gap: "0.75rem" }}>
            {state.messages.map((m, i) => (
              <Bubble key={i} role={m.role} content={m.content} />
            ))}
            {state.pending && <p className="muted small">…</p>}
            {state.error && <p className="err">{state.error}</p>}
          </div>
        )}
      </div>
      <Composer draft={draft} setDraft={setDraft} onSubmit={send} disabled={state.pending} />
    </div>
  );
}

function Bubble({ role, content }: { role: string; content: string }) {
  const mine = role === "user";
  return (
    <div style={{ display: "flex", justifyContent: mine ? "flex-end" : "flex-start" }}>
      <div
        className="card"
        style={{
          maxWidth: "80%",
          background: mine ? "var(--surface-2)" : "var(--surface)",
          padding: "0.6rem 0.85rem",
        }}
      >
        {content}
      </div>
    </div>
  );
}

export function Composer({
  draft,
  setDraft,
  onSubmit,
  disabled,
}: {
  draft: string;
  setDraft: (v: string) => void;
  onSubmit: (e: React.FormEvent) => void;
  disabled: boolean;
}) {
  return (
    <form
      onSubmit={onSubmit}
      style={{ borderTop: "1px solid var(--border)", padding: "0.9rem 1.5rem" }}
    >
      <div className="row" style={{ gap: "0.6rem", maxWidth: 760, margin: "0 auto" }}>
        <input
          className="input"
          style={{ flex: 1 }}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="Describe what you need…"
          autoFocus
        />
        <button className="btn btn-primary" disabled={disabled || !draft.trim()}>
          Send
        </button>
      </div>
    </form>
  );
}
```

- [ ] **Step 3: Verify build + lint**

Run: `cd web && npm run lint && npm run build`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add "web/app/(chat)/layout.tsx" "web/app/(chat)/chat/page.tsx"
git commit -m "feat(web): /chat new-RFQ intake conversation"
```

---

## Task 8: `/chat/[rfqId]` route — created thread with inline quotes

**Files:**
- Create: `web/app/(chat)/chat/[rfqId]/page.tsx`

**Interfaces:**
- Consumes: `getRfq`/`rfqQuotes`/`acceptQuote`/`QuoteRow`/`RFQ` (`web/lib/api.ts`), `Composer` (Task 7 export), `useToken`.
- Produces: a created RFQ thread — replays transcript, renders inline quote cards, polls quotes every ~4s until closed, Accept → order.

- [ ] **Step 1: `web/app/(chat)/chat/[rfqId]/page.tsx`**

```tsx
"use client";

import { useEffect, useRef, useState } from "react";
import { useParams } from "next/navigation";
import { useToken } from "@/lib/auth";
import { acceptQuote, getRfq, rfqQuotes, type QuoteRow, type RFQ } from "@/lib/api";

const POLL_MS = 4000;

export default function ChatThread() {
  const token = useToken();
  const { rfqId } = useParams<{ rfqId: string }>();
  const [rfq, setRfq] = useState<RFQ | null>(null);
  const [quotes, setQuotes] = useState<QuoteRow[]>([]);
  const [msg, setMsg] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const closedRef = useRef(false);

  useEffect(() => {
    if (!token || !rfqId) return;
    getRfq(token, rfqId).then((r) => {
      setRfq(r.rfq);
      closedRef.current = r.rfq.status === "closed";
    }).catch(() => {});
  }, [token, rfqId]);

  useEffect(() => {
    if (!token || !rfqId) return;
    let active = true;
    const poll = () =>
      rfqQuotes(token, rfqId).then((r) => active && setQuotes(r.quotes)).catch(() => {});
    poll();
    const t = setInterval(() => {
      if (closedRef.current) return;
      poll();
    }, POLL_MS);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, [token, rfqId]);

  const accept = async (quoteId: string) => {
    if (!token) return;
    setBusy(quoteId);
    try {
      const { order_id, total_cents } = await acceptQuote(token, quoteId);
      setMsg(`✅ Order created — ${order_id} for $${(total_cents / 100).toFixed(2)}.`);
      closedRef.current = true;
      const r = await getRfq(token, rfqId);
      setRfq(r.rfq);
      const q = await rfqQuotes(token, rfqId);
      setQuotes(q.quotes);
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "accept failed");
    } finally {
      setBusy(null);
    }
  };

  if (!rfq) return <div style={{ padding: "1.5rem" }} className="muted">Loading…</div>;

  const closed = rfq.status === "closed";

  return (
    <div style={{ flex: 1, overflowY: "auto", padding: "1.5rem", maxWidth: 760, width: "100%", margin: "0 auto" }}>
      <div className="col" style={{ gap: "0.75rem" }}>
        {(rfq.messages ?? []).map((m, i) => (
          <div key={i} style={{ display: "flex", justifyContent: m.role === "user" ? "flex-end" : "flex-start" }}>
            <div className="card" style={{ maxWidth: "80%", background: m.role === "user" ? "var(--surface-2)" : "var(--surface)", padding: "0.6rem 0.85rem" }}>
              {m.content}
            </div>
          </div>
        ))}

        <div className="card">
          <div className="row" style={{ justifyContent: "space-between" }}>
            <strong>Quotes</strong>
            <span className="badge">{rfq.status}</span>
          </div>
          {quotes.length === 0 ? (
            <p className="muted small" style={{ marginTop: "0.5rem" }}>Sellers&apos; agents are responding…</p>
          ) : (
            <div className="col" style={{ gap: "0.5rem", marginTop: "0.6rem" }}>
              {quotes.map((q, i) => (
                <div key={q.id} className="row" style={{ justifyContent: "space-between", alignItems: "center", borderTop: i ? "1px solid var(--border)" : "none", paddingTop: i ? "0.5rem" : 0 }}>
                  <div style={{ minWidth: 0 }}>
                    <strong>{q.currency} {(q.price_cents / 100).toFixed(2)}</strong>
                    <span className="muted small"> · {q.seller_name ?? q.seller_company_id.slice(0, 8)} · {q.delivery_days != null ? `${q.delivery_days}d` : "—"}</span>
                    {q.notes && <div className="muted small">{q.notes}</div>}
                  </div>
                  {q.status === "accepted" ? (
                    <span className="badge badge-success">accepted</span>
                  ) : q.status === "rejected" ? (
                    <span className="badge">rejected</span>
                  ) : !closed ? (
                    <button className="btn btn-sm btn-primary" disabled={busy === q.id} onClick={() => accept(q.id)}>
                      {busy === q.id ? "…" : "Accept"}
                    </button>
                  ) : null}
                </div>
              ))}
            </div>
          )}
        </div>

        {msg && <p className="ok">{msg}</p>}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Verify build + lint**

Run: `cd web && npm run lint && npm run build`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add "web/app/(chat)/chat/[rfqId]/page.tsx"
git commit -m "feat(web): /chat/[rfqId] thread with inline polled quotes + accept"
```

---

## Task 9: Route buyers to `/chat` (redirects + nav)

**Files:**
- Modify: `web/app/login/page.tsx`, `web/app/register/page.tsx` (post-auth destination)
- Modify: `web/app/page.tsx` (landing CTA)
- Modify: `web/components/AppShell.tsx` (buyer nav → Chat)
- Modify: `web/app/(app)/dashboard/page.tsx` (buyer action → /chat)
- Create: `web/lib/landing.ts` (shared destination helper)

**Interfaces:**
- Consumes: `Me` (`web/lib/api.ts`), `me` fetch.
- Produces: `landingPath(me: Me): string` — `/chat` for buyer/both/unset, `/dashboard` for seller-only.

- [ ] **Step 1: Shared helper `web/lib/landing.ts`**

```ts
import type { Me } from "./api";

// Buyers (buyer/both/unset) land in the chat; seller-only companies keep the
// classic dashboard/AppShell.
export function landingPath(me: Me): string {
  return me.company_type === "seller" ? "/dashboard" : "/chat";
}
```

- [ ] **Step 2: Login redirect — `web/app/login/page.tsx`**

After a successful login the page currently does `router.replace("/dashboard")` once the token is set. Replace that with a fetch of `me` then `landingPath`. Locate the success branch (around line 22) and change it to:

```ts
      setToken(res.token);
      const profile = await me(res.token);
      router.replace(landingPath(profile));
```

Add imports: `import { me, setToken } from "@/lib/api";` (merge with existing `@/lib/api` import) and `import { landingPath } from "@/lib/landing";`. If the file already calls `setToken`, keep that call and only add the `me`+`landingPath` lines. Ensure the handler is `async`.

- [ ] **Step 3: Register redirect — `web/app/register/page.tsx`**

Same change at the success branch (around line 40): after the token is stored, `const profile = await me(token); router.replace(landingPath(profile));`. Add the `landingPath` import and `me` import as needed.

- [ ] **Step 4: Landing CTA — `web/app/page.tsx`**

The static "Go to dashboard →" link (line ~29) points at `/dashboard`. Change its `href` to `/chat` and its label to "Open chat →" so a logged-in buyer lands in the chat. (This page is static and role-unaware; `/chat`'s ChatShell guards auth and seller-only users can still reach `/dashboard` from the chat sidebar.)

- [ ] **Step 5: Buyer nav in `web/components/AppShell.tsx`**

In the `NAV` array, change the Buyer group item so buyers who are in `AppShell` (i.e. `both` companies) get a link into the chat:

```ts
  {
    group: "Buyer",
    items: [{ href: "/chat", label: "RFQ chat", show: isBuyer }],
  },
```

(The old `/rfqs` pages remain reachable by URL but drop out of the nav.)

- [ ] **Step 6: Dashboard buyer action — `web/app/(app)/dashboard/page.tsx`**

Change the buyer `<Action href="/rfqs" title="Post an RFQ" .../>` to `href="/chat"` and body "Describe what you need in plain language and get ranked quotes."

- [ ] **Step 7: Verify build + lint**

Run: `cd web && npm run lint && npm run build`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add web/lib/landing.ts web/app/login/page.tsx web/app/register/page.tsx web/app/page.tsx web/components/AppShell.tsx "web/app/(app)/dashboard/page.tsx"
git commit -m "feat(web): route buyers to /chat after auth + nav"
```

---

## Self-Review

**Spec coverage:**
- §3 conversational intake endpoint → Tasks 2–4 (heuristic + LLM intake, `POST /v1/rfq-chat`, reuse of `CreateAndMatch`, ~3-turn cap, explicit-create). ✓
- §3 migration `rfqs.messages jsonb NULL` + transcript persisted only on create + returned by `GET /v1/rfqs/:id` → Task 1 + Task 4. ✓
- §4 ChatShell (sidebar threads + `＋ New RFQ` + bottom user menu + right-side status dot) → Task 6; `/chat` + `/chat/[rfqId]` routes → Tasks 7–8; buyers land on `/chat`, sellers/admins keep AppShell → Task 9. ✓
- §4 client additions (`rfqChat`, `messages?` on RFQ, reuse `listRfqs`/`rfqQuotes`/`acceptQuote`) → Tasks 5, 8. ✓
- §4 data flow: intake loop, route to `/chat/<id>` on create, poll quotes ~4s, Accept → order → Tasks 7–8. ✓
- §5 resilience (error bubble w/ preserved message, silent quote-poll failure, offline dot, terminal states) → reducer `error` + Composer keeps draft on failure path, `StatusDot`, "Sellers' agents are responding…" empty state, accepts disabled when closed. ✓
- §6 testing: Go heuristic asks-then-creates + auth guard + transcript persistence (Tasks 1, 2, 4); frontend build gate + reducer unit test (Task 5). ✓
- §6 migration applies/reverts; existing RFQs (null messages) still load → Task 1 (`,omitempty`, nil-guarded decode). ✓

**Deviations flagged (need a human call if they matter):**
1. "tenant-guard rejects non-buyers" is implemented as the same guard as `/v1/rfqs` (auth + tenant), tested via the 401 path — see the Global Constraints scope note.
2. openai_compatible intake is fully implemented (parity with anthropic), reusing `openai.go`'s request/response types.

**Placeholder scan:** no TBD/TODO; every code step is complete. ✓
**Type consistency:** `ChatMessage` (Go `Role`/`Content`; TS `role`/`content`), `IntakeDecision.{Action,Message,Title,Text}`, `ChatResult.{Done,Assistant,RFQ,MatchCount}`, `SetRFQMessagesParams.{Messages,ID}`, reducer actions `send`/`reply`/`error`/`reset`, `landingPath(me)` are used identically across the tasks that produce and consume them. ✓
```
