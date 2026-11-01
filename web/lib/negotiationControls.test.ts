import { describe, expect, it } from "vitest";
import { canManagePause, NOT_PAUSED, pauseDetail, pauseHeadline, type PauseInfo } from "./negotiationControls";

const paused = (over: Partial<PauseInfo>): PauseInfo => ({ ...NOT_PAUSED, paused: true, ...over });

describe("negotiation kill switch helpers", () => {
  it("says nothing while negotiation runs", () => {
    expect(pauseHeadline(NOT_PAUSED)).toBe("");
    expect(pauseDetail(null)).toBe("");
  });

  it("names who paused it, platform first", () => {
    expect(pauseHeadline(paused({ paused_by: "company", company_paused: true }))).toContain("your company");
    const both = paused({ paused_by: "platform", platform_paused: true, company_paused: true, platform_reason: "Incident" });
    expect(pauseHeadline(both)).toContain("TradeBridge");
    expect(pauseDetail(both)).toMatch(/^Reason: Incident\. /);
  });

  it("only lets owners and admins flip the company switch", () => {
    expect(canManagePause("owner")).toBe(true);
    expect(canManagePause("admin")).toBe(true);
    expect(canManagePause("member")).toBe(false);
    expect(canManagePause(undefined)).toBe(false);
  });
});
