// Impersonation hand-off: installs a read-only support token where the main
// buyer/seller app reads its own auth token (lib/api.ts's setToken), so the
// current tab's main-app session logs in as the impersonated company. The
// server (EnforceReadOnly) blocks all writes regardless of what happens here
// client-side — this is only the support-flow UX.

import { setToken, clearToken } from "./api";

export const IMPERSONATING_FLAG_KEY = "tb_impersonating";

// startImpersonation installs the read-only token via the app's own token
// store (so subscribers/useToken pick it up), marks the session as
// impersonating (so AppShell shows a banner), and opens the app in a new tab.
export function startImpersonation(token: string, companyName: string): void {
  setToken(token);
  localStorage.setItem(IMPERSONATING_FLAG_KEY, companyName);
  window.open("/dashboard", "_blank");
}

// leaveImpersonation clears the installed token + flag, ending the read-only
// support session in this tab.
export function leaveImpersonation(): void {
  clearToken();
  localStorage.removeItem(IMPERSONATING_FLAG_KEY);
}
