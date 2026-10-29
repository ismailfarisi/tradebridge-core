import { describe, expect, it } from "vitest";
import type { Me } from "./api";
import { isActive, pageCrumb, visibleNav } from "./nav";

const me = (company_type: Me["company_type"]): Me => ({ company_id: "c", user_id: "u", role: "owner", company_type });
const hrefs = (m: Me) => visibleNav(m).flatMap((s) => s.items.map((i) => i.href));

describe("visibleNav", () => {
  it("orders buyer pages by the deal flow, without New RFQ or settings", () => {
    expect(hrefs(me("buyer"))).toEqual(["/dashboard", "/rfqs", "/negotiations", "/orders", "/savings"]);
  });
  it("shows sellers their selling pages and drops empty groups", () => {
    const nav = visibleNav(me("seller"));
    expect(nav.map((s) => s.group)).toEqual(["", "Selling"]);
    expect(hrefs(me("seller"))).toEqual(["/dashboard", "/orders", "/seller/profile", "/seller/catalog", "/seller/agent"]);
  });
});

describe("isActive", () => {
  it("matches nested pages", () => {
    expect(isActive("/orders/123", "/orders")).toBe(true);
    expect(isActive("/ordersx", "/orders")).toBe(false);
  });
  it("puts the negotiation room under Negotiations, not RFQs", () => {
    expect(isActive("/rfqs/1/negotiation", "/negotiations")).toBe(true);
    expect(isActive("/rfqs/1/negotiation", "/rfqs")).toBe(false);
    expect(isActive("/rfqs/1", "/rfqs")).toBe(true);
  });
});

describe("pageCrumb", () => {
  it("titles top-level pages", () => {
    expect(pageCrumb("/savings")).toEqual({ title: "Savings" });
    expect(pageCrumb("/settings/billing/")).toEqual({ title: "Billing & usage" });
  });
  it("gives detail pages a way back", () => {
    expect(pageCrumb("/rfqs/9/negotiation")).toEqual({ title: "Negotiation", back: { href: "/negotiations", label: "Negotiations" } });
    expect(pageCrumb("/rfqs/9").back?.href).toBe("/rfqs");
    expect(pageCrumb("/orders/9").back?.href).toBe("/orders");
  });
  it("falls back to no title", () => {
    expect(pageCrumb("/unknown/page")).toEqual({ title: "" });
  });
});
