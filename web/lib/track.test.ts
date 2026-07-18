import { describe, expect, it } from "vitest";
import { trackEvent } from "./track";

describe("trackEvent", () => {
  it("returns true when consent is accepted", () => {
    expect(trackEvent("page_view", {}, "accepted")).toBe(true);
  });

  it("returns false when consent is rejected", () => {
    expect(trackEvent("page_view", {}, "rejected")).toBe(false);
  });

  it("returns false when consent is null", () => {
    expect(trackEvent("page_view", {}, null)).toBe(false);
  });

  it("defaults props to an empty object", () => {
    expect(trackEvent("page_view", undefined, "accepted")).toBe(true);
  });
});
