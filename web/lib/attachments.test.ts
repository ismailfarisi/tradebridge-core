import { describe, expect, it } from "vitest";
import { validateFile, MAX_FILE_BYTES } from "./attachments";

describe("validateFile", () => {
  it("accepts a small png", () => {
    expect(validateFile("a.png", "image/png", 1024)).toBeNull();
  });
  it("accepts a pdf", () => {
    expect(validateFile("spec.pdf", "application/pdf", 1024)).toBeNull();
  });
  it("rejects disallowed types", () => {
    expect(validateFile("a.zip", "application/zip", 10)).toMatch(/not supported/i);
  });
  it("rejects oversize files", () => {
    expect(validateFile("big.png", "image/png", MAX_FILE_BYTES + 1)).toMatch(/10 MB/);
  });
});
