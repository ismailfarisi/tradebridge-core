import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  daysLeft,
  getSellerRfq,
  listSellerRfqs,
  parseCategories,
  parseDeliveryDays,
  parsePrice,
  sellerLandingPath,
  sellerRfqsPath,
  sellerState,
  submitSellerQuote,
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

describe("seller quote form helpers", () => {
  it("parses typed prices into major units", () => {
    expect(parsePrice("12,500.50")).toBe(12500.5);
    expect(parsePrice("AED 12500")).toBe(12500);
    expect(parsePrice(" 99 ")).toBe(99);
    expect(parsePrice("0")).toBeNull();
    expect(parsePrice("-5")).toBeNull();
    expect(parsePrice("12.345")).toBeNull();
    expect(parsePrice("abc")).toBeNull();
    expect(parsePrice("")).toBeNull();
  });

  it("parses optional delivery days", () => {
    expect(parseDeliveryDays("")).toBeNull();
    expect(parseDeliveryDays(" 7 ")).toBe(7);
    expect(parseDeliveryDays("0")).toBe(0);
    expect(parseDeliveryDays("3.5")).toBeUndefined();
    expect(parseDeliveryDays("-1")).toBeUndefined();
    expect(parseDeliveryDays("5000")).toBeUndefined();
  });

  it("posts the quote and surfaces structured errors", async () => {
    mockFetch(201, JSON.stringify({ revised: false, quote: { id: "q1", status: "submitted", price_cents: 1250050, currency: "AED" } }));
    const out = await submitSellerQuote("tok", "r1", { price: 12500.5, currency: "AED", delivery_days: 7 });
    expect(out.quote.id).toBe("q1");
    const [url, init] = (global.fetch as unknown as { mock: { calls: [string, RequestInit][] } }).mock.calls[0];
    expect(url).toMatch(/\/v1\/seller\/rfqs\/r1\/quote$/);
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ price: 12500.5, currency: "AED", delivery_days: 7 });

    mockFetch(403, JSON.stringify({ code: "verify_email", message: "verify your email to continue" }));
    await expect(submitSellerQuote("tok", "r1", { price: 1, currency: "AED" })).rejects.toThrow("verify your email to continue");
  });
});
