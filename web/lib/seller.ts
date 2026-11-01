// Seller-side RFQ inbox client: the RFQs Core matched to the caller's company,
// plus the helpers the seller sign-in/sign-up pages share.

import { CORE_URL, errorMessage, type AttachmentMeta, type CompanyType, type Me } from "./api";

export type SellerTab = "" | "open" | "quoted" | "closed";

export type SellerQuote = {
  id: string;
  status: string;
  price_cents: number;
  currency: string;
  delivery_days?: number | null;
  notes?: string;
  version?: number;
  updated_at?: string;
};

export type SellerRequirements = {
  category?: string;
  quantity?: string;
  specs?: string[] | null;
  location?: string;
  deadline?: string;
  keywords?: string[] | null;
};

export type SellerRFQ = {
  id: string;
  title: string;
  raw_text: string;
  requirements: SellerRequirements;
  category: string;
  status: string;
  created_at: string;
  expires_at: string | null;
  match_score: number;
  match_reason: string;
  matched_at: string;
  unseen?: boolean;
  quote: SellerQuote | null;
};

async function authed<T>(token: string, path: string): Promise<T> {
  const res = await fetch(`${CORE_URL}${path}`, {
    headers: { authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  const text = await res.text();
  if (!res.ok) throw new Error(errorMessage(text, res.status));
  return (text ? JSON.parse(text) : undefined) as T;
}

export function sellerRfqsPath(params: { tab?: SellerTab; q?: string } = {}): string {
  const qs = new URLSearchParams();
  if (params.tab) qs.set("tab", params.tab);
  if (params.q?.trim()) qs.set("q", params.q.trim());
  const s = qs.toString();
  return `/v1/seller/rfqs${s ? `?${s}` : ""}`;
}

export const listSellerRfqs = (token: string, params: { tab?: SellerTab; q?: string } = {}) =>
  authed<{ rfqs: SellerRFQ[]; count: number }>(token, sellerRfqsPath(params));

export const getSellerRfq = (token: string, id: string) =>
  authed<{ rfq: SellerRFQ; attachments: AttachmentMeta[] }>(token, `/v1/seller/rfqs/${encodeURIComponent(id)}`);

export const isSellerType = (t?: CompanyType) => t === "seller" || t === "both";

// Where a seller lands after signing in. Buyer-only accounts that used the
// seller sign-in go to their normal buyer landing (the chat).
export function sellerLandingPath(me: Me): string {
  return isSellerType(me.company_type) ? "/seller/rfqs" : "/chat";
}

// Splits the free-text "what do you supply" field into catalog categories.
export function parseCategories(text: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of text.split(/[,\n]/)) {
    const c = raw.trim();
    const key = c.toLowerCase();
    if (c && !seen.has(key)) {
      seen.add(key);
      out.push(c);
    }
  }
  return out;
}

// What the seller should do next on an RFQ, as a short badge label.
export function sellerState(r: Pick<SellerRFQ, "status" | "quote">): { label: string; cls: string } {
  if (r.quote?.status === "accepted") return { label: "Won", cls: "badge badge-success" };
  if (r.status === "closed" || r.status === "expired") {
    return { label: r.status === "expired" ? "Expired" : "Closed", cls: "badge" };
  }
  if (r.quote) return { label: "Quoted", cls: "badge badge-info" };
  return { label: "Awaiting quote", cls: "badge badge-warning" };
}

// Days left before an RFQ stops accepting quotes, or null when unknown/past.
export function daysLeft(expiresAt: string | null, now = new Date()): number | null {
  if (!expiresAt) return null;
  const ms = new Date(expiresAt).getTime() - now.getTime();
  if (!Number.isFinite(ms) || ms <= 0) return null;
  return Math.ceil(ms / 86_400_000);
}

// --- seller quote form ---

export type SellerQuoteInput = {
  price: number; // major units, e.g. 12500.5
  currency: string;
  delivery_days?: number | null;
  notes?: string;
};

export type SellerQuoteResult = { revised: boolean; quote: SellerQuote };

// Currencies offered in the quote form. AED first: UAE/GCC is the home market.
export const QUOTE_CURRENCIES = ["AED", "SAR", "QAR", "OMR", "KWD", "BHD", "USD", "EUR", "GBP"] as const;

// submitSellerQuote sends the seller's first quote, or a revised offer when
// they already have a live quote on this RFQ.
export async function submitSellerQuote(token: string, rfqId: string, input: SellerQuoteInput): Promise<SellerQuoteResult> {
  const res = await fetch(`${CORE_URL}/v1/seller/rfqs/${encodeURIComponent(rfqId)}/quote`, {
    method: "POST",
    headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
    body: JSON.stringify(input),
    cache: "no-store",
  });
  const text = await res.text();
  if (!res.ok) throw new Error(errorMessage(text, res.status));
  return JSON.parse(text) as SellerQuoteResult;
}

// parsePrice reads a typed amount ("12,500.50", "AED 12500") as major units.
// Returns null for anything that is not a positive number.
export function parsePrice(text: string): number | null {
  const cleaned = text.replace(/[A-Za-z\s,]/g, "");
  if (!/^\d+(\.\d{1,2})?$/.test(cleaned)) return null;
  const n = Number(cleaned);
  return Number.isFinite(n) && n > 0 ? n : null;
}

// parseDeliveryDays reads the optional delivery field: "" means not given
// (null), a whole number of days from 0 to 3650 is accepted, anything else is
// undefined (invalid).
export function parseDeliveryDays(text: string): number | null | undefined {
  const t = text.trim();
  if (t === "") return null;
  if (!/^\d+$/.test(t)) return undefined;
  const n = Number(t);
  return n <= 3650 ? n : undefined;
}
