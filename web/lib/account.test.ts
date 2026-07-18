import { describe, it, expect } from "vitest";
import { canDelete } from "./account";

describe("canDelete", () => {
  it("is true for owner", () => {
    expect(canDelete("owner")).toBe(true);
  });
  it("is false for admin", () => {
    expect(canDelete("admin")).toBe(false);
  });
  it("is false for member", () => {
    expect(canDelete("member")).toBe(false);
  });
  it("is false for empty string", () => {
    expect(canDelete("")).toBe(false);
  });
});
