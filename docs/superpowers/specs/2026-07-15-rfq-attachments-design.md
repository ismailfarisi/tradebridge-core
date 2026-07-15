# RFQ Attachments (Images + PDFs) — Design

**Date:** 2026-07-15
**Status:** Approved
**Repos:** tradebridge-core (Go) + tradebridge-agent (Bun/TS)

## Goal

Buyers can attach images (JPEG/PNG/WebP/GIF) and PDFs to an RFQ — both when
creating it in the web chat and when replying in a quote thread. Both LLM
stages see the real files: the Core intake agent uses them to extract
requirements, and the seller agent views them when quoting and asks
clarifying questions if details are missing. The clarification loop is closed
end-to-end (today it is one-way: the agent never sees the buyer's reply).

## Out of scope (YAGNI)

- Inbound email intake (no email pipeline exists; web chat only).
- S3/object storage — local disk now, behind an interface that allows a swap.
- Attachment dedupe by hash (sha256 is stored, dedupe is not implemented).
- Orphaned-upload cleanup sweep (age-based GC can be added later).
- OCR / vision for non-Anthropic providers beyond what their APIs support.

## 1. Storage & data model (Core)

New migration:

```sql
CREATE TABLE attachments (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  company_id  uuid NOT NULL REFERENCES companies(id),  -- uploader
  filename    text NOT NULL,
  mime        text NOT NULL,
  size_bytes  bigint NOT NULL,
  sha256      text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE quote_messages ADD COLUMN attachments jsonb; -- [{id,filename,mime,size_bytes}]
```

- File bytes live on local disk at `ATTACHMENTS_DIR` (default
  `~/tradebridge-data/attachments/<id>`), never in Postgres. Disk I/O goes
  through a small `BlobStore` interface (Put/Get/Delete) so S3 can replace it
  later without touching callers.
- `rfqs.messages` jsonb entries gain an optional `attachment_ids: []` field
  (no migration needed; it is jsonb).
- Limits: allowlist `image/jpeg, image/png, image/webp, image/gif,
  application/pdf`; max 10 MB per file; max 5 attachments per message.
  MIME is verified by sniffing file magic bytes, not trusting the client
  header.

## 2. Upload & download endpoints (Core)

- `POST /v1/attachments` — multipart form, authenticated (buyer session).
  Validates mime/size, writes bytes to disk, inserts row, returns
  `{id, filename, mime, size_bytes}`. Upload-first: the chat/thread POST
  bodies stay JSON and reference attachment IDs.
- `GET /v1/attachments/:id` — web download; caller must be a party to the
  RFQ/quote the attachment is referenced by (buyer company or matched
  seller company).
- `GET /agent/v1/attachments/:id` — gateway route, agent Bearer token; the
  agent's company must be matched to the RFQ (or party to the quote) that
  references the attachment.

Authorization rule for both download routes: an attachment is visible to its
uploader, and to any company that is a party to an RFQ/quote whose
messages reference the attachment ID.

## 3. Intake vision (Core, Go)

- `chatReq` messages gain `attachment_ids: []string`.
- The Anthropic intake client (`internal/rfq/claude.go`, model
  `claude-haiku-4-5`) builds multimodal content: `image` content blocks
  (base64 from disk) for images and `document` blocks for PDFs, alongside the
  text.
- The requirements extractor gets the same multimodal input so
  `rfqs.requirements` benefits from attached specs.
- Fallbacks: OpenAI-compatible providers get images as data-URI `image_url`
  parts; PDFs and the heuristic (no-key) path degrade to a text marker
  `[attached: <filename>]`. Documented limitation.

## 4. Events (Core → agent)

- `rfq.matched` payload gains
  `attachments: [{id, filename, mime, size_bytes}]` (all attachments
  referenced by the RFQ's intake messages).
- New event type **`quote.message`**: emitted to the seller company's
  `agent_events` when a buyer posts in a quote thread
  (`internal/quotes/handler.go addMessage`). Payload:
  `{rfq_id, quote_id, author, body, attachments}`. This closes the
  clarification loop — today no event is emitted and the agent never sees
  the buyer's answer.

## 5. Seller agent (Bun/TS)

- **Fetch:** new `fetchAttachment(id)` helper hits
  `GET /agent/v1/attachments/:id` with the agent token; bytes cached under
  `~/.tradebridge-agent/attachments/<id>` to avoid re-downloading across
  clarification rounds.
- **Providers (`src/reasoning/provider.ts`):** message content becomes
  block-structured. `AnthropicProvider` sends native `image` and `document`
  (PDF) blocks. `OpenAICompatibleProvider` sends images as data-URI
  `image_url` parts and PDFs as text extracted with the existing `unpdf`
  helper (`src/ingestion/extract.ts`). Heuristic path uses filenames only.
- **Quoting (`src/reasoning/quote.ts`):** `generateQuote` receives the RFQ's
  attachments and includes them in the LLM call; the existing
  quote-or-clarification JSON contract is unchanged.
- **New `quote.message` handler (`src/runtime.ts`):** on a buyer message
  (author = buyer), re-run the quote decision with the full thread transcript
  plus all attachments (initial + newly attached). Idempotent via the
  existing ProcessedEvent ledger. Result is either `quote.submit` or another
  `clarification.request`.
- **Clarification cap:** max 2 clarification rounds per RFQ (tracked on the
  agent's `Rfq` entity); after the cap, the agent must quote or `no_bid`.

## 6. Buyer web UI (Next.js, tradebridge-core/web)

- One shared `AttachmentComposer` component: paperclip button, file picker
  restricted to images/PDF, client-side size check, preview chips with
  remove, uploads to `POST /v1/attachments` then includes the returned IDs in
  the message send.
- Used in both the RFQ chat composer and the quote-thread reply box.
- Transcript rendering: image attachments show as thumbnails (authenticated
  download URL), PDFs as a named file chip that opens/downloads.

## 7. Failure handling

- Upload: bad mime/oversize/too-many → clear 400s; disk write failure → 500,
  no orphan row (insert after successful write).
- Agent fetch or vision failure: degrade gracefully — the prompt gets
  `[attachment unavailable: <filename>]` and quoting proceeds; the failure is
  logged, never blocks the quote pipeline.
- `quote.message` emission failure must not fail the buyer's message POST
  (message row is the source of truth; event emission is best-effort with
  logging, consistent with existing event emission).

## 8. Testing

**Core (Go):**
- Upload handler: mime sniffing, size/count limits, auth.
- Download handlers: party-scoping (uploader, matched seller, stranger 403).
- Intake: fake LLM asserting image/document blocks are constructed from
  attachment IDs.
- `quote.message` event emitted on buyer post with correct payload; not
  emitted on seller post.

**Agent (Bun):**
- Provider adapters: block construction for images and PDFs (Anthropic and
  OpenAI-compatible shapes).
- `quote.message` handler: re-quote with thread context, idempotency,
  clarification cap enforcement.
- PDF text-extraction fallback path.

## Architecture flow

```
Buyer web chat ──POST /v1/attachments──▶ Core BlobStore (disk) + attachments row
      │ chat POST {messages, attachment_ids}
      ▼
Core intake LLM (multimodal: image/document blocks) → requirements → CreateAndMatch
      ▼ agent_events: rfq.matched {…, attachments[]}
Seller agent ──GET /agent/v1/attachments/:id──▶ bytes → vision quote
      ├─ quote.submit ──────────────▶ quotes
      └─ clarification.request ─────▶ quote_messages (seller)
Buyer replies in thread (text + attachments)
      ▼ NEW agent_events: quote.message {body, attachments[]}
Seller agent re-quotes with full thread (cap: 2 clarification rounds)
```
