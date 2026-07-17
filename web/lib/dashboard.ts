import { CORE_URL } from "./api";

export type RecentRFQ = {
  id: string; title: string; status: string;
  quote_count: number; new_quotes: number; created_at: string;
};
export type BuyerDashboard = {
  active_rfqs: number; quotes_received: number; new_quotes: number;
  orders_in_progress: number; recent_rfqs: RecentRFQ[];
};
export type SellerDashboard = {
  incoming_rfqs: number; review_queue: number; active_orders: number;
  agent: { online: boolean; last_seen: string | null; autonomy: boolean };
  plan_usage: {
    plan: string; rfq_used: number; rfq_limit: number;
    catalog_used: number; catalog_limit: number; seats_used: number; seats_limit: number;
  };
};

async function authed<T>(token: string, path: string): Promise<T> {
  const res = await fetch(`${CORE_URL}${path}`, {
    headers: { authorization: `Bearer ${token}` }, cache: "no-store",
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return (text ? JSON.parse(text) : undefined) as T;
}

export const getBuyerDashboard = (token: string) => authed<BuyerDashboard>(token, "/v1/dashboard/buyer");
export const getSellerDashboard = (token: string) => authed<SellerDashboard>(token, "/v1/dashboard/seller");
