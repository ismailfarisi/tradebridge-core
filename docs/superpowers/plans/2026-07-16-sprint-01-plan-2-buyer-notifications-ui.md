# Sprint 1 — Plan 2: Buyer-App Notifications UI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Give the buyer web app (`core/web`) a visible notifications experience: a notification bell (unread badge + dropdown), a verify-email banner + the `/verify` page the email links to, and a public waitlist signup on the marketing landing — all against the live Core API.

**Architecture:** Components co-located in `core/web/components` (NOT a shared package — see decisions). A new `lib/notifications.ts` API client mirrors the existing `lib/api.ts` `req()` pattern (token passed as arg, `NEXT_PUBLIC_CORE_URL` base). The bell polls `GET /v1/notifications`; the banner reads a new `email_verified` field on `/v1/me`. Mounts go in the `AppShell` and `ChatShell` headers.

**Tech Stack:** Next.js 16.2.10 (App Router), React 19.2.4, TypeScript 5 strict, plain global CSS (CSS variables + utility classes in `app/globals.css`), vitest (lib tests only). Backend: Go/Fiber Core (already built in Plan 1).

## Decisions (from brainstorming, 2026-07-16)
- **Buyer app only; seller app deferred.** `agent/web` talks to the Bun Agent backend with an Agent token and cannot hit Core `/v1/notifications`; seller-side notifications need a separate backend design and are OUT of this plan.
- **Co-locate, don't package.** Build components in `core/web/components`; defer any `@tradebridge/ui` extraction until a second compatible consumer exists.
- **Reuse the existing design system.** Use the CSS variables (`--primary`, `--surface`, `--danger`, `--muted`, `--border`, `--radius`, …) and utility classes (`.card`, `.btn`, `.btn-sm`, `.btn-ghost`, `.badge`, `.badge-info`, `.row`, `.muted`, `.small`) already in `app/globals.css`. Do NOT add Tailwind, CSS Modules, or new global styles beyond a couple of scoped additions if truly needed.

## Global Constraints
- **Repo:** everything is in `tradebridge-core` (the buyer app is `tradebridge-core/web`). One branch.
- **Core API base:** frontend uses `CORE_URL = process.env.NEXT_PUBLIC_CORE_URL ?? "http://localhost:8080"` (`lib/api.ts:6`). Dev API on `:8080`, web on `:3010`.
- **Auth:** JWT in localStorage key `tb_core_token`; hooks `useToken()` / `useMe()` from `lib/auth.ts`; token is passed **as an argument** to each API function (mirror this — do not read the token globally inside `lib/notifications.ts`).
- **Fiber error shape:** non-OK responses are plain text; the `req()` helper throws `Error(text)`. Reuse `req()` semantics.
- **Testing reality:** `core/web` has vitest for **lib logic only** — there is NO React Testing Library / jsdom component harness, and existing tests (`lib/*.test.ts`) test pure functions. So: TDD the Go change (integration test) and the `lib/notifications.ts` client (vitest, mocking `fetch`), and verify COMPONENTS by `npm run build` (typecheck/compile) + driving the running app. Do NOT introduce RTL/jsdom (out of scope; don't unilaterally add a test framework).
- **Component conventions:** `"use client"` at top of interactive components; PascalCase files in `components/`; default-export the component; reuse existing inline-style + utility-class style; `@/*` path alias.
- **Notifications API (from Plan 1, live):** `GET /v1/notifications?limit&offset` → `{notifications:[{id,type,title,body,data,read,created_at}], unread}`; `POST /v1/notifications/:id/read` → 204; `POST /v1/notifications/read-all` → 204; `GET/PUT /v1/notifications/preferences`. `data` may carry `{cta_label, cta_path, rfq_id, order_id}`. Verify: `POST /v1/auth/verify-email {token}` (public) → 200; `POST /v1/auth/resend-verification` (guarded) → `{sent:true}`. Waitlist: `POST /v1/waitlist {email, source?}` (public) → `{joined:true}`.

---

### Task 1: Core — expose `email_verified` on `/v1/me`

**Files:**
- Modify: `internal/httpapi/router.go` (the `/v1/me` handler, ~lines 90-102)
- Test: `test/integration/api_me_verified_test.go` (new)

**Interfaces:**
- Produces: `/v1/me` response gains `"email_verified": bool` (true when the user's `email_verified_at` is non-null).

**Why:** the verify-email banner must know if the current user is verified; `/v1/me` is what the app already calls on load.

- [ ] **Step 1: Write the failing integration test** `test/integration/api_me_verified_test.go`

```go
package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestMe_ReportsEmailVerified(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)

	_, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "MeCo", "company_type": "buyer",
		"email": uniqueEmail("me-"), "password": "password123",
	})
	token := reg["token"].(string)
	userID := reg["user_id"].(string)

	// New user is unverified.
	status, me := doJSON(t, app, "GET", "/v1/me", token, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, false, me["email_verified"])

	// After verifying, /v1/me flips to true.
	verifyUser(t, pool, userID)
	_ = q
	status, me = doJSON(t, app, "GET", "/v1/me", token, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, true, me["email_verified"])
}
```

- [ ] **Step 2: Run RED**

Run: `cd /home/deploy/b2b/tradebridge-core && TEST_DATABASE_URL="postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable" go test ./test/integration/ -run TestMe_ReportsEmailVerified -v`
Expected: FAIL — `email_verified` absent (nil).

- [ ] **Step 3: Implement.** In `internal/httpapi/router.go`, the `/v1/me` handler currently builds `out := fiber.Map{company_id,user_id,role}` and adds company name/type via `GetCompanyByID`. Add a user lookup for the verified flag:

```go
// after the company lookup block, before return c.JSON(out):
if u, err := q.GetUserByID(c.Context(), middleware.UserID(c)); err == nil {
    out["email_verified"] = u.EmailVerifiedAt.Valid
}
```

- [ ] **Step 4: Run GREEN**

Run: same as Step 2. Expected: PASS. Then `go build ./...` and a quick full suite: `TEST_DATABASE_URL=... go test ./... 2>&1 | tail -5` (all green).

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi/router.go test/integration/api_me_verified_test.go
git commit -m "feat(core): expose email_verified on /v1/me for the verify banner"
```

---

### Task 2: `lib/notifications.ts` API client + `Me.email_verified` type

**Files:**
- Create: `web/lib/notifications.ts`
- Create: `web/lib/notifications.test.ts`
- Modify: `web/lib/api.ts` (add `email_verified?: boolean` to the `Me` type)

**Interfaces:**
- Produces:
  - `type Notification = { id: string; type: string; title: string; body: string; data?: Record<string, unknown>; read: boolean; created_at: string }`
  - `type NotificationList = { notifications: Notification[]; unread: number }`
  - `listNotifications(token: string, opts?: { limit?: number; offset?: number }): Promise<NotificationList>`
  - `markNotificationRead(token: string, id: string): Promise<void>`
  - `markAllNotificationsRead(token: string): Promise<void>`
  - `resendVerification(token: string): Promise<void>` (POST /v1/auth/resend-verification)
  - `verifyEmail(token: string): Promise<void>` (POST /v1/auth/verify-email {token}) — NOTE: takes the EMAIL token, no auth header
  - `joinWaitlist(email: string, source?: string): Promise<void>` (POST /v1/waitlist, no auth)
- Consumes: `CORE_URL` from `lib/api.ts`.

- [ ] **Step 1: Write the failing test** `web/lib/notifications.test.ts` (mock `fetch`, mirror `lib/chat.test.ts` style)

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { listNotifications, markNotificationRead, joinWaitlist } from "./notifications";

const realFetch = global.fetch;
beforeEach(() => { vi.restoreAllMocks(); });
afterEach(() => { global.fetch = realFetch; });

function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("notifications client", () => {
  it("parses the list + unread count", async () => {
    mockFetch(200, JSON.stringify({ notifications: [{ id: "n1", type: "rfq.matched", title: "T", body: "B", read: false, created_at: "2026-07-16T00:00:00Z" }], unread: 1 }));
    const out = await listNotifications("tok");
    expect(out.unread).toBe(1);
    expect(out.notifications[0].id).toBe("n1");
    expect((global.fetch as any).mock.calls[0][1].headers.authorization).toBe("Bearer tok");
  });

  it("mark-read tolerates a 204 empty body", async () => {
    mockFetch(204, "");
    await expect(markNotificationRead("tok", "n1")).resolves.toBeUndefined();
  });

  it("joinWaitlist posts without an auth header", async () => {
    mockFetch(200, JSON.stringify({ joined: true }));
    await joinWaitlist("a@b.com", "landing");
    const init = (global.fetch as any).mock.calls[0][1];
    expect(init.headers.authorization).toBeUndefined();
    expect(JSON.parse(init.body)).toEqual({ email: "a@b.com", source: "landing" });
  });
});
```

- [ ] **Step 2: Run RED**

Run: `cd /home/deploy/b2b/tradebridge-core/web && npx vitest run lib/notifications.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement `web/lib/notifications.ts`**

```ts
import { CORE_URL } from "./api";

export type Notification = {
  id: string;
  type: string;
  title: string;
  body: string;
  data?: Record<string, unknown>;
  read: boolean;
  created_at: string;
};
export type NotificationList = { notifications: Notification[]; unread: number };

// Local request helper (lib/api.ts's req() is not exported). Mirrors its
// semantics: JSON on success, plain-text error → throw; auth header optional.
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
  return (text ? JSON.parse(text) : null) as T;
}

export function listNotifications(token: string, opts: { limit?: number; offset?: number } = {}): Promise<NotificationList> {
  const qs = new URLSearchParams();
  if (opts.limit != null) qs.set("limit", String(opts.limit));
  if (opts.offset != null) qs.set("offset", String(opts.offset));
  const q = qs.toString();
  return call<NotificationList>("GET", `/v1/notifications${q ? `?${q}` : ""}`, { token });
}

export function markNotificationRead(token: string, id: string): Promise<void> {
  return call<void>("POST", `/v1/notifications/${id}/read`, { token });
}

export function markAllNotificationsRead(token: string): Promise<void> {
  return call<void>("POST", `/v1/notifications/read-all`, { token });
}

export function resendVerification(token: string): Promise<void> {
  return call<void>("POST", `/v1/auth/resend-verification`, { token });
}

// Consumes the EMAIL verification token (public route, no auth header).
export function verifyEmail(emailToken: string): Promise<void> {
  return call<void>("POST", `/v1/auth/verify-email`, { body: { token: emailToken } });
}

export function joinWaitlist(email: string, source?: string): Promise<void> {
  return call<void>("POST", `/v1/waitlist`, { body: { email, ...(source ? { source } : {}) } });
}
```

- [ ] **Step 4: Add `email_verified` to `Me`** in `web/lib/api.ts`:
```ts
export type Me = {
  company_id: string;
  user_id: string;
  role: string;
  company_name?: string;
  company_type?: CompanyType;
  email_verified?: boolean;
};
```

- [ ] **Step 5: Run GREEN**

Run: `cd /home/deploy/b2b/tradebridge-core/web && npx vitest run lib/notifications.test.ts` (PASS) and `npx tsc --noEmit` (clean).

- [ ] **Step 6: Commit**

```bash
git add web/lib/notifications.ts web/lib/notifications.test.ts web/lib/api.ts
git commit -m "feat(web): notifications API client + email_verified on Me type"
```

---

### Task 3: `NotificationBell` component + mount in both shells

**Files:**
- Create: `web/components/NotificationBell.tsx`
- Modify: `web/components/AppShell.tsx` (mount in header, before Sign out), `web/components/ChatShell.tsx` (same)

**Interfaces:**
- Produces: `export default function NotificationBell({ token }: { token: string })` — a `"use client"` component that polls `listNotifications` every 30s (and on dropdown open), shows a bell button with an unread-count badge, and a dropdown listing recent notifications (title, body, relative time); clicking an item marks it read (and navigates to `data.cta_path` if present); a "Mark all read" action calls `markAllNotificationsRead`.
- Consumes: `listNotifications`, `markNotificationRead`, `markAllNotificationsRead` (Task 2); Next `useRouter`, `Link`.

- [ ] **Step 1: Implement `web/components/NotificationBell.tsx`**

```tsx
"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  listNotifications,
  markNotificationRead,
  markAllNotificationsRead,
  type Notification,
} from "@/lib/notifications";

const POLL_MS = 30_000;

export default function NotificationBell({ token }: { token: string }) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Notification[]>([]);
  const [unread, setUnread] = useState(0);
  const boxRef = useRef<HTMLDivElement | null>(null);

  const refresh = useCallback(async () => {
    try {
      const out = await listNotifications(token, { limit: 15 });
      setItems(out.notifications);
      setUnread(out.unread);
    } catch {
      /* best-effort; leave prior state */
    }
  }, [token]);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, POLL_MS);
    return () => clearInterval(id);
  }, [refresh]);

  // Close on outside click.
  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onClick);
    return () => document.removeEventListener("mousedown", onClick);
  }, [open]);

  async function onItem(n: Notification) {
    if (!n.read) {
      try { await markNotificationRead(token, n.id); } catch { /* ignore */ }
      setItems((xs) => xs.map((x) => (x.id === n.id ? { ...x, read: true } : x)));
      setUnread((u) => Math.max(0, u - 1));
    }
    const path = typeof n.data?.cta_path === "string" ? (n.data.cta_path as string) : null;
    if (path) { setOpen(false); router.push(path); }
  }

  async function onMarkAll() {
    try { await markAllNotificationsRead(token); } catch { /* ignore */ }
    setItems((xs) => xs.map((x) => ({ ...x, read: true })));
    setUnread(0);
  }

  return (
    <div ref={boxRef} style={{ position: "relative" }}>
      <button
        className="btn btn-sm btn-ghost"
        aria-label={`Notifications${unread ? ` (${unread} unread)` : ""}`}
        onClick={() => { setOpen((o) => !o); if (!open) refresh(); }}
        style={{ position: "relative" }}
      >
        <span aria-hidden>🔔</span>
        {unread > 0 && (
          <span
            style={{
              position: "absolute", top: -4, right: -4, minWidth: 16, height: 16,
              padding: "0 4px", borderRadius: 8, background: "var(--danger)", color: "#fff",
              fontSize: 10, lineHeight: "16px", textAlign: "center", fontWeight: 700,
            }}
          >
            {unread > 9 ? "9+" : unread}
          </span>
        )}
      </button>

      {open && (
        <div
          className="card"
          style={{
            position: "absolute", right: 0, top: "calc(100% + 8px)", width: 340, maxHeight: 420,
            overflowY: "auto", zIndex: 50, padding: 0,
          }}
        >
          <div className="row" style={{ justifyContent: "space-between", padding: "0.6rem 0.8rem", borderBottom: "1px solid var(--border)" }}>
            <strong>Notifications</strong>
            {unread > 0 && <button className="btn btn-sm btn-ghost" onClick={onMarkAll}>Mark all read</button>}
          </div>
          {items.length === 0 ? (
            <div className="muted small" style={{ padding: "1rem 0.8rem" }}>No notifications yet.</div>
          ) : (
            items.map((n) => (
              <button
                key={n.id}
                onClick={() => onItem(n)}
                style={{
                  display: "block", width: "100%", textAlign: "left", cursor: "pointer",
                  background: n.read ? "transparent" : "var(--surface-2)",
                  border: "none", borderBottom: "1px solid var(--border)", padding: "0.6rem 0.8rem", color: "inherit",
                }}
              >
                <div className="row" style={{ justifyContent: "space-between", gap: "0.5rem" }}>
                  <strong style={{ fontSize: 13 }}>{n.title}</strong>
                  {!n.read && <span className="dot" aria-hidden style={{ background: "var(--primary)" }} />}
                </div>
                <div className="muted small" style={{ marginTop: 2 }}>{n.body}</div>
              </button>
            ))
          )}
        </div>
      )}
    </div>
  );
}
```
(If `.dot` doesn't accept a background override cleanly, use an inline 8px circle span instead. If `--surface-2` is absent, use `var(--surface)`.)

- [ ] **Step 2: Mount in `web/components/AppShell.tsx`** — in the header's right side, insert the bell just before the Sign out button. The header currently ends with the `<button ... >Sign out</button>`. Wrap them in a `.row`:
```tsx
<div className="row" style={{ gap: "0.6rem" }}>
  <NotificationBell token={token} />
  <button className="btn btn-sm btn-ghost" onClick={() => { logout(); router.replace("/login"); }}>
    Sign out
  </button>
</div>
```
Import `NotificationBell` and ensure `token` is in scope (AppShell already uses `useMe()`/`useToken()` — use that token; render the bell only when `token` is truthy).

- [ ] **Step 3: Mount in `web/components/ChatShell.tsx`** — same pattern in its header's right-side row (ChatShell also has `useMe`/token in scope).

- [ ] **Step 4: Verify (build + drive)**

Run: `cd /home/deploy/b2b/tradebridge-core/web && npx tsc --noEmit && npm run build` — must compile clean.
Then a manual smoke (see Task 7): with the API + web running, a logged-in buyer who has notifications sees the badge; opening the dropdown lists them; clicking marks read; the badge decrements.

- [ ] **Step 5: Commit**

```bash
git add web/components/NotificationBell.tsx web/components/AppShell.tsx web/components/ChatShell.tsx
git commit -m "feat(web): notification bell with unread badge + dropdown in app & chat shells"
```

---

### Task 4: `VerifyEmailBanner` component + mount

**Files:**
- Create: `web/components/VerifyEmailBanner.tsx`
- Modify: `web/components/AppShell.tsx`, `web/components/ChatShell.tsx` (render above `<main>`)

**Interfaces:**
- Produces: `export default function VerifyEmailBanner({ token, emailVerified }: { token: string; emailVerified: boolean | undefined })` — renders nothing when `emailVerified !== false`; otherwise a full-width notice bar with a "Resend verification email" button (calls `resendVerification`, shows a "Sent — check your inbox" confirmation) and a session-dismiss "×".
- Consumes: `resendVerification` (Task 2).

- [ ] **Step 1: Implement `web/components/VerifyEmailBanner.tsx`**

```tsx
"use client";

import { useState } from "react";
import { resendVerification } from "@/lib/notifications";

export default function VerifyEmailBanner({
  token, emailVerified,
}: { token: string; emailVerified: boolean | undefined }) {
  const [dismissed, setDismissed] = useState(false);
  const [sent, setSent] = useState(false);
  const [busy, setBusy] = useState(false);

  if (emailVerified !== false || dismissed) return null;

  async function resend() {
    setBusy(true);
    try { await resendVerification(token); setSent(true); } catch { /* ignore */ } finally { setBusy(false); }
  }

  return (
    <div
      className="notice"
      style={{
        display: "flex", alignItems: "center", justifyContent: "space-between", gap: "1rem",
        padding: "0.6rem 1.5rem", borderBottom: "1px solid var(--border)",
        background: "color-mix(in srgb, var(--warning) 15%, var(--surface))",
      }}
    >
      <span className="small">
        <strong>Verify your email</strong> to post RFQs. Check your inbox for the verification link.
      </span>
      <span className="row" style={{ gap: "0.5rem" }}>
        {sent ? (
          <span className="small muted">Sent — check your inbox.</span>
        ) : (
          <button className="btn btn-sm" onClick={resend} disabled={busy}>
            {busy ? "Sending…" : "Resend email"}
          </button>
        )}
        <button className="btn btn-sm btn-ghost" aria-label="Dismiss" onClick={() => setDismissed(true)}>×</button>
      </span>
    </div>
  );
}
```
(If `color-mix` is unsupported by the target browsers, fall back to a flat `var(--surface-2)` background with a `var(--warning)` left border.)

- [ ] **Step 2: Mount** in `AppShell.tsx` and `ChatShell.tsx` — render `<VerifyEmailBanner token={token} emailVerified={me.email_verified} />` immediately BEFORE the `<main>` element (full-width, under the header). `me` and `token` come from the existing `useMe()`.

- [ ] **Step 3: Verify (build + drive)** — `npx tsc --noEmit && npm run build` clean. Manual: a freshly-registered (unverified) buyer sees the banner; "Resend" shows "Sent"; after verifying, the banner disappears on next load.

- [ ] **Step 4: Commit**

```bash
git add web/components/VerifyEmailBanner.tsx web/components/AppShell.tsx web/components/ChatShell.tsx
git commit -m "feat(web): verify-email banner with resend in app & chat shells"
```

---

### Task 5: `/verify` page (email link target)

**Files:**
- Create: `web/app/verify/page.tsx`

**Interfaces:**
- Produces: a public route `/verify?token=…` that consumes the email token via `verifyEmail(token)` and shows success (with a link to `/dashboard`) or an error (invalid/expired, with a note to sign in and resend).
- Consumes: `verifyEmail` (Task 2).

- [ ] **Step 1: Implement `web/app/verify/page.tsx`**

```tsx
"use client";

import { useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import Link from "next/link";
import { verifyEmail } from "@/lib/notifications";

export default function VerifyPage() {
  const params = useSearchParams();
  const token = params.get("token");
  const [state, setState] = useState<"pending" | "ok" | "error">("pending");

  useEffect(() => {
    if (!token) { setState("error"); return; }
    let active = true;
    verifyEmail(token)
      .then(() => active && setState("ok"))
      .catch(() => active && setState("error"));
    return () => { active = false; };
  }, [token]);

  return (
    <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center", padding: "2rem" }}>
      <div className="card" style={{ maxWidth: 420, textAlign: "center" }}>
        {state === "pending" && <p className="muted">Verifying your email…</p>}
        {state === "ok" && (
          <>
            <h2>Email verified ✓</h2>
            <p className="muted">Your account is now fully active.</p>
            <Link className="btn btn-primary" href="/dashboard">Go to dashboard</Link>
          </>
        )}
        {state === "error" && (
          <>
            <h2>Verification failed</h2>
            <p className="muted">This link is invalid or has expired. Sign in and resend the verification email from the banner.</p>
            <Link className="btn" href="/login">Sign in</Link>
          </>
        )}
      </div>
    </div>
  );
}
```
(Next 16 may require the `useSearchParams` component to be wrapped in a `<Suspense>` boundary for static generation — if `npm run build` errors on that, wrap the inner content in `<Suspense fallback={…}>` and split into a child component, or add `export const dynamic = "force-dynamic";`.)

- [ ] **Step 2: Verify (build + drive)** — `npm run build` clean. Manual: hitting `/verify?token=<real token from the DB>` shows success and flips the user's verified state; a bad token shows the error state.

- [ ] **Step 3: Commit**

```bash
git add web/app/verify/page.tsx
git commit -m "feat(web): /verify page consuming the email verification token"
```

---

### Task 6: Waitlist signup on the marketing landing

**Files:**
- Create: `web/components/WaitlistForm.tsx`
- Modify: `web/app/page.tsx` (embed the form in the existing marketing landing)

**Interfaces:**
- Produces: `export default function WaitlistForm()` — an email input + "Join the waitlist" button calling `joinWaitlist(email, "landing")`; shows a success state ("You're on the list") and inline validation/error.
- Consumes: `joinWaitlist` (Task 2).

- [ ] **Step 1: Implement `web/components/WaitlistForm.tsx`**

```tsx
"use client";

import { useState } from "react";
import { joinWaitlist } from "@/lib/notifications";

export default function WaitlistForm() {
  const [email, setEmail] = useState("");
  const [state, setState] = useState<"idle" | "busy" | "done" | "error">("idle");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!email.includes("@")) { setState("error"); return; }
    setState("busy");
    try { await joinWaitlist(email, "landing"); setState("done"); }
    catch { setState("error"); }
  }

  if (state === "done") {
    return <p className="ok small">You&rsquo;re on the list — we&rsquo;ll be in touch.</p>;
  }

  return (
    <form onSubmit={submit} className="row" style={{ gap: "0.5rem", flexWrap: "wrap" }}>
      <input
        className="input"
        type="email"
        placeholder="you@company.com"
        value={email}
        onChange={(e) => { setEmail(e.target.value); if (state === "error") setState("idle"); }}
        aria-label="Email address"
        style={{ minWidth: 220 }}
      />
      <button className="btn btn-primary" type="submit" disabled={state === "busy"}>
        {state === "busy" ? "Joining…" : "Join the waitlist"}
      </button>
      {state === "error" && <span className="err small">Enter a valid email.</span>}
    </form>
  );
}
```

- [ ] **Step 2: Embed in `web/app/page.tsx`.** Read the current marketing landing first; add a clearly-labeled waitlist section (heading like "Get early access" + `<WaitlistForm />`) in a sensible spot (e.g., after the hero). Keep the existing content; only ADD the section + import. Do not restructure the page.

- [ ] **Step 3: Verify (build + drive)** — `npm run build` clean. Manual: submitting a valid email shows the success state; a row appears in `waitlist_signups`; resubmitting the same email still succeeds (idempotent).

- [ ] **Step 4: Commit**

```bash
git add web/components/WaitlistForm.tsx web/app/page.tsx
git commit -m "feat(web): waitlist signup form on the marketing landing"
```

---

### Task 7: End-to-end manual verification (the DoD gate)

**Files:** none (verification only). Use the `run` skill or start services manually.

- [ ] **Step 1: Start services.** Core API + gateway + web (dev DB already up):
  - API: `cd /home/deploy/b2b/tradebridge-core && APP_BASE_URL=http://localhost:3010 go run ./cmd/api` (background)
  - Web: `cd /home/deploy/b2b/tradebridge-core/web && NEXT_PUBLIC_CORE_URL=http://localhost:8080 npm run dev` (background)
- [ ] **Step 2: Drive the flows** (record what you observe):
  1. Register a new buyer → land in the app → **verify banner shows**; click **Resend** → "Sent".
  2. Grab the token from the DB (`SELECT token FROM email_verifications ...`), open `/verify?token=…` → success; reload the app → **banner gone**.
  3. Register a seller + catalog, register+verify a buyer, post an RFQ that matches → as the seller user, the **bell badge shows 1**; open dropdown → "New RFQ matched: …"; click it → marks read + navigates.
  4. On the marketing landing (`/`), submit the **waitlist** form → success; confirm a `waitlist_signups` row.
- [ ] **Step 3: Record results** in the report; capture a screenshot if the harness supports it. Fix any defect found (loop back to the relevant task).
- [ ] **Step 4:** No commit (verification only), unless a fix was needed.

---

## Self-Review

**Spec coverage (Plan 2 scope):** buyer notification center (Task 3) ✓; verify banner (Task 4) + `/verify` page (Task 5) ✓; waitlist landing (Task 6) ✓; the `email_verified` API dependency (Task 1) + lib client (Task 2) ✓. Seller-app notification center = explicitly DEFERRED (decision). Shared `@tradebridge/ui` package = explicitly DEFERRED (co-locate decision).

**Placeholder scan:** No TBD/TODO. Component steps carry full code. Framework-version caveats (Suspense/`useSearchParams`, `color-mix`, `--surface-2`, `.dot` override) are called out with concrete fallbacks — these are real Next 16 / CSS gotchas, not vagueness.

**Type consistency:** `Notification`/`NotificationList` shapes match the Plan 1 API (`{id,type,title,body,data,read,created_at}` + `unread`). `Me.email_verified?: boolean` (Task 2) matches the `/v1/me` field (Task 1) and the banner prop (Task 4). `token: string` threaded consistently (bell, banner) from `useMe()`.

**Testing honesty:** Tasks 1–2 are TDD (Go integration + vitest lib). Tasks 3–6 are component work verified by typecheck/build + the Task 7 manual drive — because `core/web` has no component-test harness and this plan does not introduce one. This is stated in Global Constraints, not hidden.

**Known follow-ups (not blockers):** seller-app notifications (needs Agent-backend design); extract `@tradebridge/ui` when a second consumer exists; consider RTL/jsdom if component logic grows.
