import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { getBuyerDashboard, getSellerDashboard } from "./dashboard";

const realFetch = global.fetch;
beforeEach(() => vi.restoreAllMocks());
afterEach(() => { global.fetch = realFetch; });
function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("dashboard client", () => {
  it("getBuyerDashboard sends the auth header and returns parsed counts", async () => {
    mockFetch(200, JSON.stringify({
      active_rfqs: 2, quotes_received: 5, new_quotes: 1, orders_in_progress: 1,
      recent_rfqs: [{ id: "r1", title: "Cartons", status: "open", quote_count: 3, new_quotes: 1, created_at: "2026-01-01T00:00:00Z" }],
    }));
    const d = await getBuyerDashboard("tok");
    expect(d.active_rfqs).toBe(2);
    expect(d.recent_rfqs[0].title).toBe("Cartons");
    const call = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0];
    const url = call[0] as string;
    const init = call[1] as RequestInit;
    expect(url).toContain("/v1/dashboard/buyer");
    expect((init.headers as Record<string, string>).authorization).toBe("Bearer tok");
  });

  it("getSellerDashboard sends the auth header and returns parsed data", async () => {
    mockFetch(200, JSON.stringify({
      incoming_rfqs: 4, review_queue: 2, active_orders: 1,
      agent: { online: true, last_seen: "2026-01-01T00:00:00Z", autonomy: true },
      plan_usage: { plan: "pro", rfq_used: 1, rfq_limit: 10, catalog_used: 2, catalog_limit: 50, seats_used: 1, seats_limit: 5 },
    }));
    const d = await getSellerDashboard("tok");
    expect(d.incoming_rfqs).toBe(4);
    expect(d.agent.online).toBe(true);
    expect(d.plan_usage.plan).toBe("pro");
    const call = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0];
    const url = call[0] as string;
    const init = call[1] as RequestInit;
    expect(url).toContain("/v1/dashboard/seller");
    expect((init.headers as Record<string, string>).authorization).toBe("Bearer tok");
  });

  it("throws on a non-OK response", async () => {
    mockFetch(500, "boom");
    await expect(getBuyerDashboard("tok")).rejects.toThrow();
  });
});
