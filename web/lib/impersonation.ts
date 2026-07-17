// Impersonation hand-off: installs a read-only support token where the main
// buyer/seller app reads its own auth token (lib/api.ts's setToken), so the
// current tab's main-app session logs in as the impersonated company. The
// server (EnforceReadOnly) blocks all writes regardless of what happens here
// client-side — this is only the support-flow UX.

import { setToken, clearToken } from "./api";

export const IMPERSONATING_FLAG_KEY = "tb_impersonating";

// startImpersonation installs the read-only token via the app's own token
// store (so subscribers/useToken pick it up), marks the session as
// impersonating (so AppShell shows a banner), and navigates a pre-opened tab
// to the app.
//
// `win` MUST be opened synchronously inside the triggering click handler
// (e.g. `window.open("", "_blank")` as the handler's first statement) —
// opening it only after the `await impersonate(...)` round-trip completes is
// no longer "in response to a user gesture" in Safari/Firefox and gets
// silently blocked, installing the token with nothing visibly happening.
// Returns false (and does not navigate) when `win` is null, i.e. the
// pre-open itself was blocked — the caller should surface that to the admin
// instead of assuming the hand-off worked.
export function startImpersonation(win: Window | null, token: string, companyName: string): boolean {
  setToken(token);
  localStorage.setItem(IMPERSONATING_FLAG_KEY, companyName);
  if (!win) return false;
  win.location.href = "/dashboard";
  return true;
}

// leaveImpersonation clears the installed token + flag, ending the read-only
// support session in this tab.
export function leaveImpersonation(): void {
  clearToken();
  localStorage.removeItem(IMPERSONATING_FLAG_KEY);
}
