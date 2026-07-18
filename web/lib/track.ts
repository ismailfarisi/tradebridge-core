"use client";

import { readConsent } from "./consent";

// trackEvent no-ops unless the user has accepted cookies (S9) AND a provider is wired.
// Today it is a wired stub: it returns whether it WOULD have tracked, so it's testable
// and ready to attach a provider later.
export function trackEvent(
  name: string,
  props: Record<string, unknown> = {},
  consent = readConsent()
): boolean {
  if (consent !== "accepted") return false;
  // provider hook goes here (no external provider added in S10)
  return true;
}
