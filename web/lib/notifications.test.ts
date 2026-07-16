import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { listNotifications, markNotificationRead, joinWaitlist } from "./notifications";

const realFetch = global.fetch;
beforeEach(() => { vi.restoreAllMocks(); });
afterEach(() => { global.fetch = realFetch; });

function mockFetch(status: number, body: string) {
  // Null-body statuses (204/205/304) can't carry a body per the WHATWG fetch
  // spec — Node's undici enforces this even for an empty string, so mirror a
  // real 204 (no body at all) rather than passing "" through.
  const nullBody = status === 204 || status === 205 || status === 304;
  global.fetch = vi.fn(async () => new Response(nullBody ? null : body, { status })) as unknown as typeof fetch;
}

describe("notifications client", () => {
  it("parses the list + unread count", async () => {
    mockFetch(200, JSON.stringify({ notifications: [{ id: "n1", type: "rfq.matched", title: "T", body: "B", read: false, created_at: "2026-07-16T00:00:00Z" }], unread: 1 }));
    const out = await listNotifications("tok");
    expect(out.unread).toBe(1);
    expect(out.notifications[0].id).toBe("n1");
    expect((global.fetch as any).mock.calls[0][1].headers.authorization).toBe("Bearer tok");
  });

  it("mark-read tolerates a 204 empty body", async () => {
    mockFetch(204, "");
    await expect(markNotificationRead("tok", "n1")).resolves.toBeUndefined();
  });

  it("joinWaitlist posts without an auth header", async () => {
    mockFetch(200, JSON.stringify({ joined: true }));
    await joinWaitlist("a@b.com", "landing");
    const init = (global.fetch as any).mock.calls[0][1];
    expect(init.headers.authorization).toBeUndefined();
    expect(JSON.parse(init.body)).toEqual({ email: "a@b.com", source: "landing" });
  });
});
