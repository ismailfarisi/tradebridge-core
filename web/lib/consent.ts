"use client";

const KEY = "tb_cookie_consent";

export type Consent = "accepted" | "rejected";

type ConsentStore = Pick<Storage, "getItem" | "setItem">;

function defaultStore(): ConsentStore | undefined {
  if (typeof window === "undefined") return undefined;
  return window.localStorage;
}

export function readConsent(store: ConsentStore | undefined = defaultStore()): Consent | null {
  if (!store) return null;
  const v = store.getItem(KEY);
  return v === "accepted" || v === "rejected" ? v : null;
}

export function writeConsent(v: Consent, store: ConsentStore | undefined = defaultStore()): void {
  if (store) store.setItem(KEY, v);
}

// needsConsent is true until the user has made a choice.
export function needsConsent(store: ConsentStore | undefined = defaultStore()): boolean {
  return readConsent(store) === null;
}
