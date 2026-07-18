import { describe, expect, it } from "vitest";
import { npsCategory, npsScore } from "./feedback";

describe("npsCategory", () => {
  it("classifies 10 as promoter", () => {
    expect(npsCategory(10)).toBe("promoter");
  });
  it("classifies 8 as passive", () => {
    expect(npsCategory(8)).toBe("passive");
  });
  it("classifies 5 as detractor", () => {
    expect(npsCategory(5)).toBe("detractor");
  });
});

describe("npsScore", () => {
  it("computes % promoters - % detractors, rounded", () => {
    expect(npsScore([10, 10, 0])).toBe(33);
  });
  it("returns 0 for no rows", () => {
    expect(npsScore([])).toBe(0);
  });
});
