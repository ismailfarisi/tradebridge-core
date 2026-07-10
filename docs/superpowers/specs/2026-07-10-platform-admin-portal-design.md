# Platform Admin Portal (global model + system-prompt control) — Design

- **Date:** 2026-07-10
- **Status:** Approved (design) — pending implementation plan
- **Repo:** `tradebridge-core` (Go Core API + Next.js web)

## 1. Goal

Give the **platform operator** a dedicated, separately-authenticated admin portal to
control the **global LLM configuration** (provider/model/base-URL/key) and the
**system prompts** that drive RFQ requirement-extraction and the chat-intake
assistant. This is a platform-wide superadmin — distinct from a company's
`owner`/`admin` role — and it lives behind its own login and credential store so
no company user (buyer, seller, or company admin) can reach it.

## 2. Scope

**In scope:** a separate admin credential store + login + JWT realm; a
`PlatformAdminGuard`; a global `platform_settings` store (model config + two
system prompts); repointing the RFQ extractor/intake resolver at the global
store; threading the editable prompts into the LLM calls; an `/admin` portal
(login + settings) in the existing web app; removal of the now-superseded
company admin section.

**Out of scope (YAGNI):** per-company model overrides (removed — global only);
multiple platform admins / admin management UI (single bootstrapped admin for
v1); usage/fraud dashboards (were unbuilt skeletons — removed, not rebuilt);
password reset / MFA for the admin; a separate admin subdomain (portal lives in
the same app at `/admin`).

**Decisions settled in brainstorming:** platform superadmin (global scope) ·
**separate dedicated admin login** (own credential store + token) · portal in the
**same web app at `/admin`** · v1 controls **global model settings + system
prompts** · the per-company model page is **not** kept (superseded by global).

## 3. Admin identity & auth (isolated realm)

**Credential store:** new table `platform_admins(id TEXT PK, email TEXT UNIQUE,
password_hash TEXT, created_at TIMESTAMPTZ)`. **Bootstrapped on Core API boot**
from env `PLATFORM_ADMIN_EMAIL` + `PLATFORM_ADMIN_PASSWORD`: if the email row is
absent it is created (password hashed with the existing bcrypt
`identity.HashPassword`); if present, boot leaves it untouched (idempotent — not a
password reset). If either env var is empty, seeding is skipped (no admin exists →
login always fails closed).

**Login:** `POST /admin/v1/login` (public) with `{email, password}` → verifies
against `platform_admins` via `identity.CheckPassword` → issues an **admin JWT**
whose claims are `{admin_id, email, scope:"platform_admin"}` (a distinct claims
type, not the company `middleware.Claims`), signed HS256 with a **separate secret
`ADMIN_JWT_SECRET`**. Returns `{token}`.

**Guard:** new `middleware.PlatformAdminGuard(secret)` parses the admin JWT with
`ADMIN_JWT_SECRET`, requires `scope == "platform_admin"`, and injects `admin_id`/
`email` into `c.Locals`. Applied to every `/admin/v1/*` route except `login`.
Because the secret and claims differ from the company realm, a company user's
token cannot pass `PlatformAdminGuard` and an admin token cannot pass the company
`JWT`/`TenantGuard` — the realms are cryptographically isolated.

**Session check:** `GET /admin/v1/me` (guarded) → `{admin_id, email}`, so the
portal can validate a stored token on load.

**Config:** `ADMIN_JWT_SECRET` (required for a usable admin realm; if empty the
router refuses to mount admin routes and logs a warning, so a misconfig fails
closed rather than signing with an empty key). Env plumbed through
`internal/config` + the `httpapi.Config` struct.

## 4. Global settings (model + system prompts)

**Store:** new singleton table `platform_settings` with a fixed single row
(enforced by a `id TEXT PK DEFAULT 'global'` + a check that only `'global'` is
used, upserted). Columns: `extraction_provider`, `extraction_model`,
`extraction_base_url`, `extraction_api_key_enc BYTEA`, `extraction_api_key_last4`,
`extraction_system_prompt`, `intake_system_prompt`, `updated_at`. The API key is
encrypted at rest with the existing `cryptobox` (AES-256-GCM); reads return only
`has_key` + `key_last4`. A blank prompt means "use the built-in default".

**Resolution change:** the RFQ pipeline currently resolves an extractor per
company via `settings.Service.ExtractionConfig(ctx, companyID)` (backed by
`company_settings`, env fallback). It is repointed at the **global** store:
`ExtractionConfig` returns the `platform_settings` row layered over the env
defaults, ignoring `companyID`. `rfq.ProviderConfig` gains two fields —
`ExtractionPrompt` and `IntakePrompt` — populated from the global row.

**Prompt threading:** `BuildExtractor(cfg, logger)` and `BuildIntake(cfg, logger)`
pass `cfg.ExtractionPrompt` / `cfg.IntakePrompt` down to the Claude/OpenAI
adapters, which use them as the system prompt when non-empty and fall back to the
existing constants (`extractSystemPrompt`, `intakeSystemPrompt`) when empty. So
editing a prompt in the portal changes live extraction/intake behavior;
"reset to default" = clear the field. The heuristic (no-key) path is unaffected —
prompts only apply to the LLM path.

**Admin API:** `GET /admin/v1/settings` → masked view
`{extraction:{provider,model,base_url,has_key,key_last4}, extraction_system_prompt,
intake_system_prompt}` (prompts returned so the editor can show current values;
if a prompt is blank the response includes the built-in default as a separate
`*_default` field so the UI can show "currently using default"). `PUT
/admin/v1/settings` upserts the row (provider validated to
`anthropic`/`openai_compatible`; `api_key` applied only when non-empty; a
`clear_api_key` flag removes it — same semantics as the existing per-company
endpoint).

**Superseded pieces:** the per-company `/v1/settings/extraction` GET/PUT routes and
the company model page are **removed**; the `settings.Service` becomes the
global resolver. The `company_settings` table is left in place (unused) to avoid a
destructive migration.

## 5. The `/admin` portal (same web app) + namespace cleanup

New `(admin)` route group in the Core web app, independent of the company
`AppShell`/`ChatShell`:

- **`web/app/(admin)/admin/login/page.tsx`** — email+password form → `POST
  /admin/v1/login` → stores the token under a **separate** localStorage key
  `tb_admin_token` (never the company `tb_core_token`). On success → `/admin`.
- **`web/app/(admin)/admin/page.tsx`** — the settings portal, guarded by the admin
  token (no/invalid token → redirect to `/admin/login`). Two panels:
  - **Model settings** — provider select, model, base-URL (openai only), API key
    (masked, blank keeps existing, "remove key" checkbox) — same UX as today's
    company model page.
  - **System prompts** — two textareas (extraction prompt, intake prompt); each
    shows the built-in default as placeholder/help text and has a **Reset to
    default** action that clears the field (server then falls back to the const).
- **`web/lib/admin.ts`** — a small client mirroring `lib/api.ts` (its own token
  store + `adminLogin`, `adminMe`, `getAdminSettings`, `updateAdminSettings`)
  targeting `/admin/v1/*`. An `AdminShell` component provides the minimal chrome
  + the token guard (own hydration guard, like `ChatShell`).

**Namespace cleanup:** the existing company `(app)/admin/*` routes are removed —
`admin/models` (superseded by the global portal), `admin/usage` + `admin/fraud`
(unbuilt skeletons). The "Admin" nav group is removed from `AppShell` and the
"Admin settings" link from `ChatShell`'s bottom menu. Company users have no admin
UI; `/admin` is exclusively the platform superadmin portal. Removing the Admin
nav leaves `isAdmin` unused in both `AppShell` and `ChatShell`, so the plan must
delete those now-dead helpers/links to keep `npm run lint` fully clean.

## 6. Security & error handling

- Realm isolation: `PlatformAdminGuard` (separate secret + `scope` claim) rejects
  company tokens and missing tokens with 401; company `TenantGuard` rejects admin
  tokens. No shared session state.
- Fail-closed: no admin seeded (missing env) or no `ADMIN_JWT_SECRET` → login/
  admin routes unusable rather than open.
- The API key is never returned in plaintext (masked `key_last4` only), matching
  the existing settings behavior.
- Login errors return a generic "invalid credentials" (no user-enumeration
  distinction between unknown email and bad password).
- Portal: failed login → inline error, password preserved for retry;
  settings-save failure → inline error, form state kept; expired/invalid admin
  token on any call → clear token + redirect to `/admin/login`.

## 7. Testing

- **Backend (Go/testify integration, real Postgres):**
  - admin bootstrap seeds a `platform_admins` row from env and is idempotent on a
    second boot;
  - `POST /admin/v1/login` returns a token for good creds and 401 for bad creds /
    unknown email;
  - `PlatformAdminGuard`: 401 with no token, 401 with a **company** token, 200
    with a valid admin token; and the company `TenantGuard` returns 401 for an
    **admin** token;
  - `PUT` then `GET /admin/v1/settings` round-trips model config + both prompts
    and never returns the key in plaintext (masked last4);
  - the extraction resolver reads the **global** row (not per-company).
- **Backend (unit):** `BuildExtractor`/`BuildIntake` use `cfg.ExtractionPrompt`/
  `cfg.IntakePrompt` when set and fall back to the constant when blank (a fake
  `llmChat`/extractor asserting the system prompt passed through).
- **Frontend:** gate = clean `npm run build` + fully clean `npm run lint`
  (matching how the web app is tested today). No new unit-test runner needed
  beyond the existing vitest.
- **Migration:** applies + reverts cleanly; existing RFQ/extraction flow still
  works with an empty `platform_settings` (env/const defaults).

## 8. Decisions locked

- Platform superadmin is a **separate realm**: `platform_admins` table, bootstrap
  from env, `POST /admin/v1/login`, admin JWT signed by `ADMIN_JWT_SECRET`,
  `PlatformAdminGuard`. Cryptographically isolated from the company realm.
- Global `platform_settings` (singleton) holds the model config + `extraction_system_prompt`
  + `intake_system_prompt`; the RFQ resolver reads it globally; prompts thread into
  `BuildExtractor`/`BuildIntake` with the current constants as fallbacks.
- Portal in the same web app: `(admin)` group, `/admin/login` + `/admin`, separate
  `tb_admin_token`, `web/lib/admin.ts` + `AdminShell`.
- The company admin section (`/admin/models`, `/admin/usage`, `/admin/fraud`) and
  the `/v1/settings/extraction` routes are removed; `company_settings` table kept
  but unused.
- No per-company overrides, no admin-management UI, no MFA/password-reset, no
  separate subdomain in v1.
