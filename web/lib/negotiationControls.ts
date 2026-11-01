import { CORE_URL, errorMessage } from "./api";

// Kill-switch state for automated negotiation. Either switch stops the agent
// from countering, accepting or auto-accepting.
export type PauseInfo = {
  platform_paused: boolean;
  platform_reason: string;
  company_paused: boolean;
  company_reason: string;
  paused: boolean;
  paused_by: "platform" | "company" | "";
  message: string;
};

export const NOT_PAUSED: PauseInfo = {
  platform_paused: false,
  platform_reason: "",
  company_paused: false,
  company_reason: "",
  paused: false,
  paused_by: "",
  message: "",
};

async function call<T>(token: string, method: string, body?: unknown): Promise<T> {
  const res = await fetch(`${CORE_URL}/v1/negotiations/controls`, {
    method,
    headers: {
      authorization: `Bearer ${token}`,
      ...(body !== undefined ? { "content-type": "application/json" } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    cache: "no-store",
  });
  const text = await res.text();
  if (!res.ok) throw new Error(errorMessage(text, res.status));
  return JSON.parse(text) as T;
}

export const getNegotiationControls = (token: string) => call<PauseInfo>(token, "GET");
export const setCompanyPause = (token: string, paused: boolean, reason = "") =>
  call<PauseInfo>(token, "PUT", { paused, reason });

// Only company owners and admins may flip the company switch (the API
// enforces this; the UI just hides the control from members).
export const canManagePause = (role: string | null | undefined) => role === "owner" || role === "admin";

export function pauseHeadline(p: PauseInfo | null | undefined): string {
  if (!p?.paused) return "";
  return p.paused_by === "platform"
    ? "Automated negotiation is paused by TradeBridge"
    : "Automated negotiation is paused by your company";
}

export function pauseDetail(p: PauseInfo | null | undefined): string {
  if (!p?.paused) return "";
  const reason = (p.paused_by === "platform" ? p.platform_reason : p.company_reason).trim();
  const tail =
    "Your agent will not send counters or accept deals until it is resumed. Supplier quotes stay open and you can still accept one by hand.";
  return reason ? `Reason: ${reason}. ${tail}` : tail;
}
