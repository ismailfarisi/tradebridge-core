import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  daysLeft,
  getSellerRfq,
  listSellerRfqs,
  parseCategories,
  sellerLandingPath,
  sellerRfqsPath,
  sellerState,
} from "./seller";

const realFetch = global.fetch;
beforeEach(() => { vi.restoreAllMocks(); });
afterEach(() => { global.fetch = realFetch; });

function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("seller RFQ client", () => {
  it("builds the list path with tab and trimmed search", () => {
    expect(sellerRfqsPath()).toBe("/v1/seller/rfqs");
    expect(sellerRfqsPath({ tab: "open" })).toBe("/v1/seller/rfqs?tab=open");
    expect(sellerRfqsPath({ tab: "", q: "  cartons " })).toBe("/v1/seller/rfqs?q=cartons");
    expect(sellerRfqsPath({ q: "   " })).toBe("/v1/seller/rfqs");
  });

  it("lists with the bearer token", async () => {
    mockFetch(200, JSON.stringify({ rfqs: [{ id: "r1", title: "T", quote: null }], count: 1 }));
    const out = await listSellerRfqs("tok", { tab: "quoted" });
    expect(out.rfqs[0].id).toBe("r1");
    const [url, init] = (global.fetch as unknown as { mock: { calls: [string, RequestInit][] } }).mock.calls[0];
    expect(url).toMatch(/\/v1\/seller\/rfqs\?tab=quoted$/);
    expect((init.headers as Record<string, string>).authorization).toBe("Bearer tok");
  });

  it("surfaces a 404 as a readable error", async () => {
    mockFetch(404, "rfq not found");
    await expect(getSellerRfq("tok", "nope")).rejects.toThrow("rfq not found");
  });
});

describe("seller helpers", () => {
  it("lands sellers on the inbox and buyers on the chat", () => {
    const base = { company_id: "c", user_id: "u", role: "owner" };
    expect(sellerLandingPath({ ...base, company_type: "seller" })).toBe("/seller/rfqs");
    expect(sellerLandingPath({ ...base, company_type: "both" })).toBe("/seller/rfqs");
    expect(sellerLandingPath({ ...base, company_type: "buyer" })).toBe("/chat");
  });

  it("parses categories from commas and newlines, deduping case-insensitively", () => {
    expect(parseCategories("Packaging, cartons\nPACKAGING, , pallets")).toEqual(["Packaging", "cartons", "pallets"]);
    expect(parseCategories("   ")).toEqual([]);
  });

  it("labels what the seller should do next", () => {
    const q = { id: "q", status: "submitted", price_cents: 100, currency: "AED" };
    expect(sellerState({ status: "open", quote: null }).label).toBe("Awaiting quote");
    expect(sellerState({ status: "quoted", quote: q }).label).toBe("Quoted");
    expect(sellerState({ status: "closed", quote: { ...q, status: "accepted" } }).label).toBe("Won");
    expect(sellerState({ status: "closed", quote: { ...q, status: "rejected" } }).label).toBe("Closed");
    expect(sellerState({ status: "expired", quote: null }).label).toBe("Expired");
  });

  it("counts days left until expiry", () => {
    const now = new Date("2026-11-01T00:00:00Z");
    expect(daysLeft(null, now)).toBeNull();
    expect(daysLeft("2026-10-31T00:00:00Z", now)).toBeNull();
    expect(daysLeft("2026-11-01T12:00:00Z", now)).toBe(1);
    expect(daysLeft("2026-11-15T00:00:00Z", now)).toBe(14);
  });
});
