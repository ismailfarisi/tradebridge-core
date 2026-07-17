import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { getThread, postCounter, postThreadMessage, streamThread } from "./thread";

const realFetch = global.fetch;
beforeEach(() => { vi.restoreAllMocks(); });
afterEach(() => { global.fetch = realFetch; });

function mockFetch(status: number, body: string) {
  global.fetch = vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("thread client", () => {
  it("getThread returns the parsed quote + messages + versions", async () => {
    mockFetch(200, JSON.stringify({
      quote: { id: "q1", price_cents: 1000, currency: "USD", status: "sent" },
      messages: [{ id: "m1", author: "buyer", body: "hi", attachments: null, created_at: "2026-01-01T00:00:00Z" }],
      versions: [{ id: "v1", version: 1, price_cents: 1000, currency: "USD", delivery_days: 5, notes: "", source: "seller", created_at: "2026-01-01T00:00:00Z" }],
    }));
    const t = await getThread("tok", "q1");
    expect(t.quote.id).toBe("q1");
    expect(t.messages).toHaveLength(1);
    expect(t.versions[0].version).toBe(1);
    const init = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][1] as RequestInit;
    expect((init.headers as Record<string, string>).authorization).toBe("Bearer tok");
    const url = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][0] as string;
    expect(url).toContain("/v1/quotes/q1/thread");
  });

  it("postCounter posts the body with the Bearer token", async () => {
    mockFetch(200, JSON.stringify({ message: { id: "m2", author: "buyer", body: "counter", attachments: null, created_at: "2026-01-01T00:00:00Z" } }));
    const r = await postCounter("tok", "q1", { price_cents: 900, delivery_days: 3, message: "counter" });
    expect(r.message.id).toBe("m2");
    const call = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0];
    const url = call[0] as string;
    const init = call[1] as RequestInit;
    expect(url).toContain("/v1/quotes/q1/counter");
    expect(init.method).toBe("POST");
    expect((init.headers as Record<string, string>).authorization).toBe("Bearer tok");
    expect(JSON.parse(init.body as string)).toEqual({ price_cents: 900, delivery_days: 3, message: "counter" });
  });

  it("postThreadMessage posts {body}", async () => {
    mockFetch(200, JSON.stringify({ id: "m3", author: "buyer", body: "hello", attachments: null, created_at: "2026-01-01T00:00:00Z" }));
    const r = await postThreadMessage("tok", "q1", "hello");
    expect(r.id).toBe("m3");
    const init = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][1] as RequestInit;
    expect(JSON.parse(init.body as string)).toEqual({ body: "hello" });
  });

  it("streamThread issues a fetch to the stream URL with the Bearer header", async () => {
    global.fetch = vi.fn(async () => new Response(null, { status: 200 })) as unknown as typeof fetch;
    const ctrl = streamThread("tok", "q1", () => {});
    expect(ctrl).toBeInstanceOf(AbortController);
    // Let the fetch microtask fire.
    await Promise.resolve();
    await Promise.resolve();
    const call = (global.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0];
    const url = call[0] as string;
    const init = call[1] as RequestInit;
    expect(url).toContain("/v1/quotes/q1/thread/stream");
    expect((init.headers as Record<string, string>).authorization).toBe("Bearer tok");
    ctrl.abort();
  });
});
