import { describe, expect, it } from "vitest";
import { dollarsToCents, money, monthLabel, statusClass } from "./negotiation";

describe("negotiation helpers", () => {
  it("formats money from cents", () => {
    expect(money(1250000)).toBe("$12,500");
    expect(money(1250050)).toBe("$12,500.50");
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
