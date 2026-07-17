import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  listOrders, getOrder, transitionOrder,
  listOrderReviews, createOrderReview,
  listOrderDisputes, createOrderDispute,
  downloadInvoice,
} from "./orders";

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

describe("orders client", () => {
  it("listOrders returns the parsed orders", async () => {
    mockFetch(200, JSON.stringify({ orders: [{ id: "o1" }] }));
    const r = await listOrders("tok");
    expect(r.orders).toHaveLength(1);
    expect(lastUrl()).toContain("/v1/orders");
    expect((lastInit().headers as Record<string, string>).authorization).toBe("Bearer tok");
  });

  it("getOrder fetches order + events", async () => {
    mockFetch(200, JSON.stringify({ order: { id: "o1" }, events: [] }));
    const r = await getOrder("tok", "o1");
    expect(r.order.id).toBe("o1");
    expect(lastUrl()).toContain("/v1/orders/o1");
  });

  it("transitionOrder POSTs to /v1/orders/o1/transitions with body and Bearer header", async () => {
    mockFetch(200, JSON.stringify({ order: { id: "o1", status: "confirmed" } }));
    await transitionOrder("tok", "o1", "confirmed");
    expect(lastUrl()).toContain("/v1/orders/o1/transitions");
    const init = lastInit();
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ to: "confirmed", note: "" });
    expect((init.headers as Record<string, string>).authorization).toBe("Bearer tok");
  });

  it("transitionOrder passes a note when given", async () => {
    mockFetch(200, JSON.stringify({ order: { id: "o1" } }));
    await transitionOrder("tok", "o1", "cancelled", "buyer changed mind");
    const init = lastInit();
    expect(JSON.parse(init.body as string)).toEqual({ to: "cancelled", note: "buyer changed mind" });
  });

  it("listOrderReviews GETs /v1/orders/:id/reviews", async () => {
    mockFetch(200, JSON.stringify({ reviews: [] }));
    await listOrderReviews("tok", "o1");
    expect(lastUrl()).toContain("/v1/orders/o1/reviews");
  });

  it("createOrderReview posts rating + body", async () => {
    mockFetch(200, JSON.stringify({ review: { id: "r1" } }));
    await createOrderReview("tok", "o1", 5, "good");
    const init = lastInit();
    expect(JSON.parse(init.body as string)).toEqual({ rating: 5, body: "good" });
  });

  it("listOrderDisputes GETs /v1/orders/:id/disputes", async () => {
    mockFetch(200, JSON.stringify({ disputes: [] }));
    await listOrderDisputes("tok", "o1");
    expect(lastUrl()).toContain("/v1/orders/o1/disputes");
  });

  it("createOrderDispute posts reason + detail", async () => {
    mockFetch(200, JSON.stringify({ dispute: { id: "d1" } }));
    await createOrderDispute("tok", "o1", "not_delivered", "never arrived");
    const init = lastInit();
    expect(JSON.parse(init.body as string)).toEqual({ reason: "not_delivered", detail: "never arrived" });
  });

  it("a non-ok response throws the body text", async () => {
    mockFetch(409, "order is not active");
    await expect(transitionOrder("tok", "o1", "confirmed")).rejects.toThrow("order is not active");
  });

  it("downloadInvoice fetches a blob with the Bearer token", async () => {
    global.fetch = vi.fn(async () => new Response(new Blob(["%PDF-1.4"]), { status: 200 })) as unknown as typeof fetch;
    const blob = await downloadInvoice("tok", "o1");
    expect(blob).toBeInstanceOf(Blob);
    expect(lastUrl()).toContain("/v1/orders/o1/invoice.pdf");
    expect((lastInit().headers as Record<string, string>).authorization).toBe("Bearer tok");
  });

  it("downloadInvoice throws the body text on error", async () => {
    global.fetch = vi.fn(async () => new Response("not found", { status: 404 })) as unknown as typeof fetch;
    await expect(downloadInvoice("tok", "o1")).rejects.toThrow("not found");
  });
});
