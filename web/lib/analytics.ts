import { CORE_URL } from "./api";

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
