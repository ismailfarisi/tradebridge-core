import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  getBillingSummary, startCheckout, inviteMember, listMembers,
  usagePercent, usageLabel, PaywallError,
} from "./billing";

const realFetch = global.fetch;
beforeEach(() => { vi.restoreAllMocks(); });
afterEach(() => { global.fetch = realFetch; });

function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("billing client", () => {
  it("parses the billing summary", async () => {
    mockFetch(200, JSON.stringify({
      plan: "free",
      entitlements: { agent_autonomy: false, catalog_max_items: 25, rfq_monthly_limit: 10, seats: 2 },
      usage: { rfq_used: 3, rfq_limit: 10, catalog_used: 1, catalog_limit: 25, seats_used: 1, seats_limit: 2 },
    }));
    const s = await getBillingSummary("tok");
    expect(s.plan).toBe("free");
    expect(s.entitlements.agent_autonomy).toBe(false);
    expect(s.usage.rfq_used).toBe(3);
  });

  it("startCheckout returns the redirect url", async () => {
    mockFetch(200, JSON.stringify({ url: "https://stub.local/checkout?x=1" }));
    const url = await startCheckout("tok", "pro", "month");
    expect(url).toContain("checkout");
    const init = (global.fetch as any).mock.calls[0][1];
    expect(JSON.parse(init.body)).toEqual({ plan: "pro", interval: "month" });
  });

  it("a 402 becomes a PaywallError carrying feature + plan", async () => {
    mockFetch(402, JSON.stringify({ code: "upgrade_required", feature: "seats", plan_required: "pro" }));
    await expect(inviteMember("tok", "a@b.com", "member")).rejects.toBeInstanceOf(PaywallError);
    try {
      mockFetch(402, JSON.stringify({ code: "upgrade_required", feature: "seats", plan_required: "pro" }));
      await inviteMember("tok", "a@b.com", "member");
    } catch (e) {
      expect(e).toBeInstanceOf(PaywallError);
      expect((e as PaywallError).feature).toBe("seats");
      expect((e as PaywallError).planRequired).toBe("pro");
    }
  });

  it("listMembers parses the users array", async () => {
    mockFetch(200, JSON.stringify({ users: [{ id: "u1", email: "a@b.com", role: "owner" }] }));
    const m = await listMembers("tok");
    expect(m[0].email).toBe("a@b.com");
  });

  it("usagePercent clamps and treats -1 as unlimited (0%)", () => {
    expect(usagePercent(5, 10)).toBe(50);
    expect(usagePercent(20, 10)).toBe(100); // clamp
    expect(usagePercent(3, -1)).toBe(0);    // unlimited
    expect(usagePercent(0, 0)).toBe(100);   // zero cap = full
  });

  it("usageLabel renders unlimited as ∞", () => {
    expect(usageLabel(3, 10)).toBe("3 / 10");
    expect(usageLabel(3, -1)).toBe("3 / ∞");
  });
});
