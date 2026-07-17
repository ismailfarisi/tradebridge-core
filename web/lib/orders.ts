import { CORE_URL } from "./api";

export type OrderStatus = "created" | "confirmed" | "fulfilled" | "completed" | "cancelled" | "disputed";
export type Order = {
  id: string; rfq_id: string; quote_id: string;
  buyer_company_id: string; seller_company_id: string;
  total_cents: number; currency: string; status: OrderStatus; created_at: string;
};
export type OrderEvent = {
  id: string; order_id: string; from_status: string; to_status: string;
  actor: string; actor_company_id: string | null; note: string; created_at: string;
};
export type Review = {
  id: string; order_id: string; reviewer_company_id: string; reviewee_company_id: string;
  direction: string; rating: number; body: string; status: string; created_at: string;
};
export type Dispute = {
  id: string; order_id: string; raised_by_company_id: string; reason: string; detail: string;
  status: string; resolution: string; resolved_by_admin_id: string | null;
  created_at: string; resolved_at: string | null;
};

// Local request helper mirroring lib/trust.ts (Fiber returns JSON on success,
// plain text on error).
async function req<T>(
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

export const listOrders = (token: string) => req<{ orders: Order[] }>("GET", "/v1/orders", { token });
export const getOrder = (token: string, id: string) =>
  req<{ order: Order; events: OrderEvent[] }>("GET", `/v1/orders/${id}`, { token });
export const transitionOrder = (token: string, id: string, to: OrderStatus, note = "") =>
  req<{ order: Order }>("POST", `/v1/orders/${id}/transitions`, { token, body: { to, note } });
export const listOrderReviews = (token: string, id: string) =>
  req<{ reviews: Review[] }>("GET", `/v1/orders/${id}/reviews`, { token });
export const createOrderReview = (token: string, id: string, rating: number, body: string) =>
  req<{ review: Review }>("POST", `/v1/orders/${id}/reviews`, { token, body: { rating, body } });
export const listOrderDisputes = (token: string, id: string) =>
  req<{ disputes: Dispute[] }>("GET", `/v1/orders/${id}/disputes`, { token });
export const createOrderDispute = (token: string, id: string, reason: string, detail: string) =>
  req<{ dispute: Dispute }>("POST", `/v1/orders/${id}/disputes`, { token, body: { reason, detail } });

// Invoice download: fetch as a blob with auth, since it's not JSON.
export async function downloadInvoice(token: string, id: string): Promise<Blob> {
  const res = await fetch(`${CORE_URL}/v1/orders/${id}/invoice.pdf`, {
    headers: { authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`);
  return res.blob();
}
