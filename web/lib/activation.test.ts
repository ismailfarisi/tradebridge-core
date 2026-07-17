import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { getActivation, shouldNudge } from "./activation";

const realFetch = global.fetch;
beforeEach(() => vi.restoreAllMocks());
afterEach(() => { global.fetch = realFetch; });
function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("activation client", () => {
  it("getActivation sends the token and parses the status", async () => {
    mockFetch(200, JSON.stringify({ role: "seller", percent_complete: 40, steps: [{ key: "profile", label: "P", done: true, cta_href: "/seller/profile", hint: "h" }] }));
    const a = await getActivation("tok");
    expect(a.role).toBe("seller");
    expect(a.percent_complete).toBe(40);
    expect(a.steps[0].done).toBe(true);
    const init = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][1] as RequestInit;
    expect((init.headers as Record<string, string>).authorization).toBe("Bearer tok");
  });
  it("getActivation throws on a non-OK response", async () => {
    mockFetch(401, "nope");
    await expect(getActivation("tok")).rejects.toThrow();
  });
});

describe("shouldNudge", () => {
  it("never nudges an unlimited plan", () => { expect(shouldNudge(9999, -1)).toBe(false); });
  it("always nudges when nothing is allowed (limit 0)", () => { expect(shouldNudge(0, 0)).toBe(true); });
  it("nudges at/over the 80% threshold", () => {
    expect(shouldNudge(8, 10)).toBe(true);
    expect(shouldNudge(7, 10)).toBe(false);
    expect(shouldNudge(10, 10)).toBe(true);
  });
});
