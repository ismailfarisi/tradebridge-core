import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { getSellerAnalytics } from "./analytics";

const realFetch = global.fetch;
beforeEach(() => vi.restoreAllMocks());
afterEach(() => { global.fetch = realFetch; });
function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("analytics client", () => {
  it("getSellerAnalytics sends the auth header and parses win_rate/top_buyers", async () => {
    mockFetch(200, JSON.stringify({
      quotes_submitted: 10, quotes_won: 4, win_rate: 0.4,
      revenue_won_cents: 400000, avg_response_hours: 3.5,
      top_buyers: [{ company_id: "b1", name: "Acme Buyer", orders: 2, revenue_cents: 200000 }],
    }));
    const a = await getSellerAnalytics("tok");
    expect(a.win_rate).toBe(0.4);
    expect(a.top_buyers[0].name).toBe("Acme Buyer");
    const call = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0];
    const url = call[0] as string;
    const init = call[1] as RequestInit;
    expect(url).toContain("/v1/analytics/seller");
    expect((init.headers as Record<string, string>).authorization).toBe("Bearer tok");
  });

  it("throws on a non-OK response", async () => {
    mockFetch(500, "boom");
    await expect(getSellerAnalytics("tok")).rejects.toThrow();
  });
});
