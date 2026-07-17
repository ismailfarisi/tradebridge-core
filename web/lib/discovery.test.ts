import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { searchDiscovery, getFacets } from "./discovery";

const realFetch = global.fetch;
beforeEach(() => vi.restoreAllMocks());
afterEach(() => { global.fetch = realFetch; });
function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("discovery client", () => {
  it("searchDiscovery builds the query and returns results", async () => {
    mockFetch(200, JSON.stringify({ results: [{ company_id: "c1", name: "Acme" }], page: 1, per_page: 20, total: 1 }));
    const r = await searchDiscovery({ q: "boxes", verified: true, category: "packaging" });
    expect(r.results[0].name).toBe("Acme");
    const url = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][0] as string;
    expect(url).toContain("/public/v1/discovery?");
    expect(url).toContain("q=boxes");
    expect(url).toContain("verified=true");
    expect(url).toContain("category=packaging");
  });
  it("getFacets returns facet buckets", async () => {
    mockFetch(200, JSON.stringify({ categories: [{ value: "packaging", count: 3 }], verified: { true: 2, false: 1 }, rating_buckets: [] }));
    const f = await getFacets({ q: "boxes" });
    expect(f.categories[0].count).toBe(3);
  });
  it("throws on a non-OK response", async () => {
    mockFetch(500, "boom");
    await expect(searchDiscovery({ q: "x" })).rejects.toThrow();
  });
});
