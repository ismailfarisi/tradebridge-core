import { CORE_URL } from "./api";
import { PaywallError } from "./billing";

export { PaywallError } from "./billing";

// Local request helper mirroring lib/billing.ts: Fiber returns JSON on
// success, plain text on error, and a 402 upgrade_required body becomes a
// PaywallError so callers can show the paywall instead of a raw error.
async function callAssist<T>(token: string, path: string, body: unknown): Promise<T> {
  const res = await fetch(`${CORE_URL}${path}`, {
    method: "POST",
    headers: { "content-type": "application/json", authorization: `Bearer ${token}` },
    body: JSON.stringify(body),
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

export const assistRFQ = (token: string, draft: string) =>
  callAssist<{ suggestion: string }>(token, "/v1/assist/rfq", { draft });

export const assistCompare = (token: string, rfqId: string) =>
  callAssist<{ explanation: string }>(token, "/v1/assist/compare", { rfq_id: rfqId });
