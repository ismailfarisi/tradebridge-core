import { describe, it, expect } from "vitest";
import { readConsent, writeConsent, needsConsent } from "./consent";

function fakeStore(initial: Record<string, string> = {}) {
  const map = new Map<string, string>(Object.entries(initial));
  return {
    getItem: (k: string) => (map.has(k) ? (map.get(k) as string) : null),
    setItem: (k: string, v: string) => {
      map.set(k, v);
    },
  };
}

describe("readConsent", () => {
  it("returns null for an unknown/garbage value", () => {
    const store = fakeStore({ tb_cookie_consent: "garbage" });
    expect(readConsent(store)).toBeNull();
  });
  it("returns null when nothing is stored", () => {
    const store = fakeStore();
    expect(readConsent(store)).toBeNull();
  });
  it('returns "accepted" when stored as accepted', () => {
    const store = fakeStore({ tb_cookie_consent: "accepted" });
    expect(readConsent(store)).toBe("accepted");
  });
  it('returns "rejected" when stored as rejected', () => {
    const store = fakeStore({ tb_cookie_consent: "rejected" });
    expect(readConsent(store)).toBe("rejected");
  });
});

describe("writeConsent", () => {
  it("persists the value so readConsent sees it", () => {
    const store = fakeStore();
    writeConsent("accepted", store);
    expect(readConsent(store)).toBe("accepted");
  });
});

describe("needsConsent", () => {
  it("is true when no choice has been made", () => {
    const store = fakeStore();
    expect(needsConsent(store)).toBe(true);
  });
  it("is false after a choice has been written", () => {
    const store = fakeStore();
    writeConsent("rejected", store);
    expect(needsConsent(store)).toBe(false);
  });
});
