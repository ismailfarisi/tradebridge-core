import { describe, expect, it } from "vitest";
import {
  deadlineDays,
  describeLimits,
  dollarsToCents,
  explainPick,
  listSaving,
  money,
  monthLabel,
  parseLimits,
  quoteRange,
  rfqStatus,
  roundDownCents,
  shortDate,
  statusClass,
  suggestTarget,
  type Supplier,
  when,
  wholeAmount,
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

describe("parseLimits", () => {
  it("reads budget, timeframe and priority", () => {
    expect(parseLimits("Up to AED 50k, need it within 2 weeks, price matters most")).toEqual({ maxCents: 5_000_000, days: 14, weight: 0.7 });
    expect(parseLimits("around 30,000 dirhams, urgent, by next week")).toEqual({ maxCents: 3_000_000, days: 7, weight: 0.3 });
    expect(parseLimits("budget 12500 for 2,000 boxes, balanced")).toEqual({ maxCents: 1_250_000, weight: 0.5 });
    expect(parseLimits("no rush")).toEqual({});
  });

  it("describes what it understood", () => {
    expect(describeLimits({ maxCents: 5_000_000, days: 14, weight: 0.7 })).toBe(`Understood: up to ${money(5_000_000)} · within 14 days · lowest price first`);
    expect(describeLimits({})).toBe("");
  });
});

describe("mandate suggestions", () => {
  it("turns an RFQ deadline into days", () => {
    expect(deadlineDays("within 10 days")).toBe(10);
    expect(deadlineDays("2026-11-12", new Date("2026-10-29T12:00:00Z"))).toBe(14);
    expect(deadlineDays("whenever")).toBeUndefined();
  });

  it("suggests a rounded target about 12% under the cap", () => {
    expect(roundDownCents(1_003_288)).toBe(1_000_000);
    expect(roundDownCents(98_765)).toBe(98_700);
    expect(suggestTarget(4_731_000)).toBe(4_160_000);
  });

  it("summarises the quote range", () => {
    expect(quoteRange([sup("A", 4_731_000, 18), sup("B", 5_315_500, 7)])).toEqual({ count: 2, minCents: 4_731_000, maxCents: 5_315_500, minDays: 7, maxDays: 18 });
    expect(quoteRange([])).toBeNull();
  });

  it("hides the saving when there was no deal", () => {
    expect(listSaving({ status: "cancelled", savings_cents: 124_500 })).toBe(0);
    expect(listSaving({ status: "completed", savings_cents: 124_500 })).toBe(124_500);
  });

  it("drops fils from stored targets", () => {
    expect(wholeAmount(7_595_808)).toBe(7_595_800);
  });

  it("formats calendar dates UAE-style", () => {
    expect(shortDate("2026-10-29T10:00:00Z", "Asia/Dubai")).toBe("29 Oct 2026");
  });

  it("labels RFQ statuses in plain words", () => {
    expect(rfqStatus("quoted")).toEqual({ label: "Quotes in", cls: "badge sc badge-info" });
    expect(rfqStatus("open").label).toBe("Waiting for quotes");
    expect(rfqStatus("on_hold").label).toBe("on hold");
  });
});
