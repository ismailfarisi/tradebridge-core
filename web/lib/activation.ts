import { CORE_URL } from "./api";

export type ActivationStep = {
  key: string;
  label: string;
  done: boolean;
  cta_href: string;
  hint: string;
};
export type Activation = {
  role: "seller" | "buyer";
  percent_complete: number;
  steps: ActivationStep[];
};

export async function getActivation(token: string): Promise<Activation> {
  const res = await fetch(`${CORE_URL}/v1/activation`, {
    headers: { authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return JSON.parse(text) as Activation;
}

// shouldNudge decides whether to show a near-limit upgrade nudge. Unlimited
// (limit < 0) never nudges; otherwise nudge once usage reaches `threshold` of
// the limit (default 80%). limit === 0 always nudges (nothing allowed on-plan).
export function shouldNudge(used: number, limit: number, threshold = 0.8): boolean {
  if (limit < 0) return false;
  if (limit === 0) return true;
  return used / limit >= threshold;
}
