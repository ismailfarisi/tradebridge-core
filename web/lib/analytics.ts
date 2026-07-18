import { CORE_URL } from "./api";
import { req } from "./admin";

export type SellerAnalytics = {
  quotes_submitted: number; quotes_won: number; win_rate: number;
  revenue_won_cents: number; avg_response_hours: number;
  top_buyers: { company_id: string; name: string; orders: number; revenue_cents: number }[];
};

export const getSellerAnalytics = async (token: string): Promise<SellerAnalytics> => {
  const res = await fetch(`${CORE_URL}/v1/analytics/seller`, {
    headers: { authorization: `Bearer ${token}` }, cache: "no-store",
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return JSON.parse(text) as SellerAnalytics;
};

// --- platform business analytics (admin console, Sprint 10) ---

export type WeekPoint = { week: string; gmv_cents: number };
export type FunnelStep = { event: string; companies: number };
export type PlanMixRow = { plan: string; status: string; count: number };

export type BusinessSummary = {
  gmv_cents: number;
  orders: number;
  gmv_by_week: WeekPoint[];
  active_buyers: number;
  active_sellers: number;
  funnel: FunnelStep[];
  plan_mix: PlanMixRow[];
  mrr_cents: number | null; // null when prices_set is false — never fabricated
  prices_set: boolean;
  active_subs: number;
  canceled_subs: number;
  churn_pct: number;
};

export const getBusinessAnalytics = (token: string) =>
  req<BusinessSummary>("GET", "/admin/v1/analytics/business", { token });
