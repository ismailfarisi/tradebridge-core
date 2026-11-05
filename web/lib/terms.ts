// Payment terms: days after delivery the buyer has to pay. 0 is cash on
// delivery; null means the party did not state terms.

export const PAYMENT_TERMS_OPTIONS = [0, 15, 30, 45, 60, 90] as const;

export function paymentTermsLabel(days: number | null | undefined): string {
  if (days == null) return "—";
  return days === 0 ? "Cash on delivery" : `Net ${days} days`;
}

// shortTermsLabel is the compact form for tables and timelines.
export function shortTermsLabel(days: number | null | undefined): string {
  if (days == null) return "—";
  return days === 0 ? "Cash" : `Net ${days}`;
}
