import { describe, expect, it } from "vitest";
import { errorMessage } from "./api";

describe("errorMessage", () => {
  it("extracts message from a structured JSON error body", () => {
    expect(
      errorMessage('{"code":"verify_email","message":"verify your email to continue"}'),
    ).toBe("verify your email to continue");
  });

  it("uses a plain-text body verbatim", () => {
    expect(errorMessage("email already registered")).toBe("email already registered");
  });

  it("falls back to the `error` field when `message` is absent", () => {
    expect(errorMessage('{"error":"forbidden"}')).toBe("forbidden");
  });

  it("uses the raw body when JSON has no message/error field", () => {
    expect(errorMessage('{"code":"x"}')).toBe('{"code":"x"}');
  });

  it("falls back to the status for an empty body", () => {
    expect(errorMessage("", 502)).toBe("HTTP 502");
  });

  it("ignores a non-string message field", () => {
    expect(errorMessage('{"message":123}')).toBe('{"message":123}');
  });
});
