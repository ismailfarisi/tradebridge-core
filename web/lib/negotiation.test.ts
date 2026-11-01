import { describe, expect, it } from "vitest";
import {
  deadlineDays,
  describeLimits,
  dollarsToCents,
  explainPick,
  money,
  monthLabel,
  parseLimits,
  priceShareOf,
  quoteRange,
  roundDownCents,
  splitWeights,
  statusClass,
  suggestTarget,
  type Supplier,
  when,
} from "./negotiation";
import { paymentTermsLabel, shortTermsLabel } from "./terms";

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
  terms: number | null = null,
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
  payment_terms_days: terms,
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

describe("payment terms", () => {
  it("mentions terms only when the mandate negotiates them and they differ", () => {
    expect(explainPick(sup("A", 1000000, 5, 60), sup("B", 1100000, 6, 0), 0.7)).toBe(
      "AED\u00a01,000 cheaper than B, and 1 day sooner.",
    );
    expect(explainPick(sup("A", 1000000, 5, 60), sup("B", 1100000, 6, 0), 0.6, { weight: 0.1 })).toBe(
      "AED\u00a01,000 cheaper than B, and 1 day sooner. Pays on net 60 days (more time to pay than cash).",
    );
    expect(explainPick(sup("Fast", 1070000, 7, 30), sup("Cheap", 1052500, 14, 30), 0.6, { weight: 0.1 })).toBe(
      "AED\u00a0175 more than Cheap, but 7 days sooner. Your mandate weighs delivery at 30%.",
    );
  });

  it("splits weights so terms come first and the rest follows the priority", () => {
    expect(splitWeights(0.7, 0.1)).toEqual({ price_weight: 0.63, terms_weight: 0.1 });
    expect(splitWeights(0.5, 0)).toEqual({ price_weight: 0.5, terms_weight: 0 });
    expect(priceShareOf({ price_weight: 0.63, terms_weight: 0.1, preferred_payment_terms_days: 60 })).toBe(0.7);
    expect(priceShareOf({ price_weight: 0.7, terms_weight: 0, preferred_payment_terms_days: null })).toBe(0.7);
  });

  it("reads payment terms from plain words without mistaking them for delivery", () => {
    expect(parseLimits("Up to AED 50k within 2 weeks, net 60")).toEqual({ maxCents: 5_000_000, days: 14, termsDays: 60 });
    expect(parseLimits("we need 45 days credit, deliver in 10 days")).toEqual({ days: 10, termsDays: 45 });
    expect(parseLimits("cash on delivery is fine")).toEqual({ termsDays: 0 });
    expect(describeLimits({ termsDays: 60 })).toBe("Understood: pay on net 60 days");
  });

  it("labels terms for people", () => {
    expect(paymentTermsLabel(0)).toBe("Cash on delivery");
    expect(paymentTermsLabel(30)).toBe("Net 30 days");
    expect(paymentTermsLabel(null)).toBe("—");
    expect(shortTermsLabel(45)).toBe("Net 45");
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
});
