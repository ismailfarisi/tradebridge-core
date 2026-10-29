import { describe, expect, it } from "vitest";
import {
  dollarsToCents,
  explainPick,
  money,
  monthLabel,
  statusClass,
  type Supplier,
  when,
} from "./negotiation";

describe("negotiation helpers", () => {
  it("formats money from cents", () => {
    expect(money(1250000)).toBe("AED\u00a012,500");
    expect(money(1250050)).toBe("AED\u00a012,500.50");
    expect(money(null)).toBe("—");
    expect(money(3625950000, "USD", { compact: true })).toBe("$36.3M");
  });

  it("parses user-entered amounts", () => {
    expect(dollarsToCents("12,500.50")).toBe(1250050);
    expect(dollarsToCents("$900")).toBe(90000);
    expect(dollarsToCents("0")).toBeNull();
    expect(dollarsToCents("abc")).toBeNull();
  });

  it("labels months and statuses", () => {
    expect(monthLabel("2026-03")).toBe("Mar");
    expect(statusClass("awaiting_approval")).toContain("warning");
    expect(statusClass("completed")).toContain("success");
  });
});

const sup = (
  name: string,
  price: number,
  delivery: number | null,
): Supplier => ({
  quote_id: name,
  seller_company_id: name,
  seller_name: name,
  verification_status: "",
  seller_avg_rating: 0,
  currency: "AED",
  initial_price_cents: price,
  initial_delivery_days: delivery,
  price_cents: price,
  delivery_days: delivery,
  version: 1,
  status: "submitted",
  counters: 0,
  last_action: "",
  utility: 0,
  recommended: false,
});

describe("explainPick", () => {
  it("names the price-for-delivery trade-off and the mandate weight", () => {
    expect(
      explainPick(sup("Desert", 1052500, 14), sup("Emirates", 1070000, 7), 0.7),
    ).toBe(
      "AED\u00a0175 cheaper than Emirates, but 7 days slower. Your mandate weighs price at 70%.",
    );
    expect(
      explainPick(sup("Fast", 1070000, 7), sup("Cheap", 1052500, 14), 0.4),
    ).toBe(
      "AED\u00a0175 more than Cheap, but 7 days sooner. Your mandate weighs delivery at 60%.",
    );
  });
  it("skips the weight when one offer wins on both", () => {
    expect(explainPick(sup("A", 1000000, 5), sup("B", 1100000, 6), 0.7)).toBe(
      "AED\u00a01,000 cheaper than B, and 1 day sooner.",
    );
    expect(
      explainPick(sup("A", 1000000, null), sup("B", 1000000, 6), 0.7),
    ).toBe("Same price as B.");
  });
});

describe("when", () => {
  it("shows day, month and time without seconds", () => {
    expect(when("2026-10-29T17:54:07Z", "Asia/Dubai")).toBe("29 Oct, 9:54 PM");
  });
});
