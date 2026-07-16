import { CORE_URL } from "./api";

export type Plan = "free" | "pro" | "business";
export type Interval = "month" | "year";

export type Entitlements = {
  agent_autonomy: boolean;
  catalog_max_items: number;
  rfq_monthly_limit: number;
  seats: number;
};
export type Usage = {
  rfq_used: number;
  rfq_limit: number;
  catalog_used: number;
  catalog_limit: number;
  seats_used: number;
  seats_limit: number;
};
export type BillingSummary = { plan: Plan; entitlements: Entitlements; usage: Usage };
export type Member = { id: string; email: string; role: string };

/** PaywallError is thrown when a gated endpoint returns 402 upgrade_required. */
export class PaywallError extends Error {
  readonly code = "upgrade_required";
  feature: string;
  planRequired: string;
  constructor(feature: string, planRequired: string) {
    super(`Upgrade required${feature ? ` for ${feature}` : ""}`);
    this.name = "PaywallError";
    this.feature = feature;
    this.planRequired = planRequired;
  }
}

// Local request helper mirroring lib/notifications.ts, but 402-aware: a
// 402 upgrade_required body becomes a PaywallError so callers can show the
// paywall instead of a raw error string.
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
  if (res.status === 402) {
    let feature = "";
    let planRequired = "pro";
    try {
      const j = JSON.parse(text) as { feature?: string; plan_required?: string };
      feature = j.feature ?? "";
      planRequired = j.plan_required ?? "pro";
    } catch {
      /* keep defaults */
    }
    throw new PaywallError(feature, planRequired);
  }
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return (text ? JSON.parse(text) : undefined) as T;
}

export function getBillingSummary(token: string): Promise<BillingSummary> {
  return call<BillingSummary>("GET", "/v1/billing/summary", { token });
}

export async function startCheckout(token: string, plan: Plan, interval: Interval): Promise<string> {
  const { url } = await call<{ url: string }>("POST", "/v1/billing/checkout", {
    token,
    body: { plan, interval },
  });
  return url;
}

export async function openBillingPortal(token: string): Promise<string> {
  const { url } = await call<{ url: string }>("POST", "/v1/billing/portal", { token });
  return url;
}

export async function listMembers(token: string): Promise<Member[]> {
  const { users } = await call<{ users: Member[] }>("GET", "/v1/users", { token });
  return users;
}

export function inviteMember(token: string, email: string, role: "admin" | "member"): Promise<{ token: string }> {
  return call<{ token: string }>("POST", "/v1/users/invite", { token, body: { email, role } });
}

/** usagePercent returns a 0–100 bar fill. -1 (unlimited) shows empty; a 0 cap shows full. */
export function usagePercent(used: number, limit: number): number {
  if (limit < 0) return 0;
  if (limit === 0) return 100;
  return Math.min(100, Math.round((used / limit) * 100));
}

/** usageLabel renders "used / limit", with ∞ for unlimited. */
export function usageLabel(used: number, limit: number): string {
  return `${used} / ${limit < 0 ? "∞" : limit}`;
}
