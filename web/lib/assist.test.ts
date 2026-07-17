import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { assistRFQ, assistCompare } from "./assist";
import { PaywallError } from "./billing";

const realFetch = global.fetch;
beforeEach(() => { vi.restoreAllMocks(); });
afterEach(() => { global.fetch = realFetch; });

function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("assist client", () => {
  it("assistRFQ posts the draft and returns the suggestion", async () => {
    mockFetch(200, JSON.stringify({ suggestion: "Add quantity + delivery deadline." }));
    const r = await assistRFQ("tok", "need 500 widgets");
    expect(r.suggestion).toContain("delivery deadline");
    const call = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0];
    const url = call[0] as string;
    const init = call[1] as RequestInit;
    expect(url).toContain("/v1/assist/rfq");
    expect(init.method).toBe("POST");
    expect((init.headers as Record<string, string>).authorization).toBe("Bearer tok");
    expect(JSON.parse(init.body as string)).toEqual({ draft: "need 500 widgets" });
  });

  it("assistCompare posts rfq_id and returns the explanation", async () => {
    mockFetch(200, JSON.stringify({ explanation: "Quote A is cheaper but slower." }));
    const r = await assistCompare("tok", "rfq1");
    expect(r.explanation).toContain("cheaper");
    const init = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][1] as RequestInit;
    expect(JSON.parse(init.body as string)).toEqual({ rfq_id: "rfq1" });
  });

  it("a 402 from assistRFQ throws PaywallError", async () => {
    mockFetch(402, JSON.stringify({ code: "upgrade_required", feature: "assist", plan_required: "pro" }));
    await expect(assistRFQ("tok", "draft")).rejects.toBeInstanceOf(PaywallError);
  });

  it("a 402 from assistCompare throws PaywallError", async () => {
    mockFetch(402, JSON.stringify({ code: "upgrade_required", feature: "assist", plan_required: "pro" }));
    await expect(assistCompare("tok", "rfq1")).rejects.toBeInstanceOf(PaywallError);
  });
});
