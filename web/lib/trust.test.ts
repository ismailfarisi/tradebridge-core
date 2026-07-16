import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  getVerification, claimDomain, checkDomain, getPublicSeller,
  createReport, createBlock,
} from "./trust";

const realFetch = global.fetch;
beforeEach(() => { vi.restoreAllMocks(); });
afterEach(() => { global.fetch = realFetch; });

function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("trust client", () => {
  it("parses verification state", async () => {
    mockFetch(200, JSON.stringify({
      verification_status: "pending", verified_domain: null, verified_at: null,
      pending: [{ domain: "acme.com", record_name: "_tradebridge-verify.acme.com", record_type: "TXT", record_value: "tradebridge-verify=abc" }],
    }));
    const s = await getVerification("tok");
    expect(s.verification_status).toBe("pending");
    expect(s.pending[0].record_value).toContain("tradebridge-verify=");
  });

  it("claimDomain posts the domain and returns the record", async () => {
    mockFetch(200, JSON.stringify({ domain: "acme.com", record_name: "_tradebridge-verify.acme.com", record_type: "TXT", record_value: "tradebridge-verify=xyz" }));
    const ch = await claimDomain("tok", "acme.com");
    expect(ch.record_type).toBe("TXT");
    const init = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][1] as RequestInit;
    expect(JSON.parse(init.body as string)).toEqual({ domain: "acme.com" });
  });

  it("checkDomain returns verified flag", async () => {
    mockFetch(200, JSON.stringify({ status: "verified", verified: true }));
    const r = await checkDomain("tok", "acme.com");
    expect(r.verified).toBe(true);
  });

  it("getPublicSeller needs no token and returns safe fields", async () => {
    mockFetch(200, JSON.stringify({
      company_id: "c1", name: "Acme", verification_status: "verified", verified_domain: "acme.com",
      description: "widgets", categories: ["widgets"], location: "NYC",
      catalog: [{ name: "A", category: "widgets", summary: null }], reputation: null,
    }));
    const s = await getPublicSeller("c1");
    expect(s.name).toBe("Acme");
    expect(s.verification_status).toBe("verified");
    expect(s.catalog).toHaveLength(1);
    const init = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][1] as RequestInit;
    expect((init.headers as Record<string, string>).authorization).toBeUndefined();
  });

  it("createReport posts reason + detail", async () => {
    mockFetch(200, JSON.stringify({ id: "r1" }));
    const r = await createReport("tok", "c2", "spam", "spammy");
    expect(r.id).toBe("r1");
    const init = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][1] as RequestInit;
    expect(JSON.parse(init.body as string)).toEqual({ reported_company_id: "c2", reason: "spam", detail: "spammy" });
  });

  it("createBlock posts blocked id", async () => {
    mockFetch(200, JSON.stringify({ status: "blocked" }));
    const r = await createBlock("tok", "c2");
    expect(r.status).toBe("blocked");
  });
});
