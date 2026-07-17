import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { listDisputes, resolveDispute, adminQuery } from "./admin";

const realFetch = global.fetch;
beforeEach(() => { vi.restoreAllMocks(); });
afterEach(() => { global.fetch = realFetch; });

function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}
function lastInit(): RequestInit {
  return (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][1] as RequestInit;
}
function lastUrl(): string {
  return (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][0] as string;
}

describe("admin dispute client", () => {
  it("listDisputes GETs /admin/v1/disputes?status=open with Bearer", async () => {
    mockFetch(200, JSON.stringify({ disputes: [{ id: "d1" }] }));
    const r = await listDisputes("tok", "open");
    expect(r.disputes).toHaveLength(1);
    expect(lastUrl()).toContain("/admin/v1/disputes?status=open");
    expect((lastInit().headers as Record<string, string>).authorization).toBe("Bearer tok");
  });

  it("listDisputes with no status omits the query string", async () => {
    mockFetch(200, JSON.stringify({ disputes: [] }));
    await listDisputes("tok");
    expect(lastUrl()).toContain("/admin/v1/disputes");
    expect(lastUrl()).not.toContain("?status=");
  });

  it("resolveDispute posts outcome/order_to/resolution to /admin/v1/disputes/d1/resolve", async () => {
    mockFetch(200, JSON.stringify({ dispute: { id: "d1", status: "resolved" } }));
    await resolveDispute("tok", "d1", "resolved", "completed", "refunded buyer");
    expect(lastUrl()).toContain("/admin/v1/disputes/d1/resolve");
    const init = lastInit();
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({
      outcome: "resolved", order_to: "completed", resolution: "refunded buyer",
    });
  });
});

describe("adminQuery", () => {
  it("omits empty and undefined params", () => {
    expect(adminQuery({ q: "acme", status: "", actor: undefined })).toBe("q=acme");
  });
  it("encodes multiple params", () => {
    const s = new URLSearchParams(adminQuery({ q: "a b", action: "company.suspend" }));
    expect(s.get("q")).toBe("a b");
    expect(s.get("action")).toBe("company.suspend");
  });
  it("returns empty string when nothing set", () => {
    expect(adminQuery({ q: "", status: undefined })).toBe("");
  });
});
