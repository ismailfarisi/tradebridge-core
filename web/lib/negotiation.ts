import { CORE_URL, errorMessage } from "./api";
import type { PauseInfo } from "./negotiationControls";
import { paymentTermsLabel, shortTermsLabel } from "./terms";

export type NegotiationStatus = "active" | "paused" | "awaiting_approval" | "completed" | "cancelled";
export type Tone = "collaborative" | "firm";

export type Strategy = {
  id: string;
  rfq_id: string;
  target_price_cents: number;
  max_price_cents: number;
  max_delivery_days: number | null;
  price_weight: number;
  max_rounds: number;
  auto_accept: boolean;
  tone: Tone;
  preferred_payment_terms_days: number | null;
  min_payment_terms_days: number | null;
  terms_weight: number;
  status: NegotiationStatus;
  outcome_quote_id: string | null;
  outcome_summary: string;
  created_at: string;
  updated_at: string;
};

export type Round = {
  id: string;
  quote_id: string;
  seller_name: string;
  round: number;
  responded_to_version: number;
  action: "counter" | "accept_ready" | "walk_away";
  seller_price_cents: number;
  seller_delivery_days: number | null;
  offer_price_cents: number | null;
  offer_delivery_days: number | null;
  seller_payment_terms_days: number | null;
  offer_payment_terms_days: number | null;
  utility: number;
  rationale: string;
  message: string;
  message_source: "template" | "llm";
  created_at: string;
};

export type Supplier = {
  quote_id: string;
  seller_company_id: string;
  seller_name: string;
  verification_status: string;
  seller_avg_rating: number;
  currency: string;
  initial_price_cents: number;
  initial_delivery_days: number | null;
  price_cents: number;
  delivery_days: number | null;
  payment_terms_days: number | null;
  version: number;
  status: string;
  counters: number;
  last_action: string;
  utility: number;
  recommended: boolean;
  // An opening far above the other suppliers (or your past price); it never
  // counts as a savings baseline.
  anchored?: boolean;
  anchor_pct?: number;
  anchor_note?: string;
};

export type BaselineBasis = "market" | "historical" | "opening_quote" | "none";

// Baselines measure one deal against every reference. Verified savings use
// the lowest applicable one; opening-quote savings are shown as secondary.
export type Baselines = {
  currency: string;
  final_cents: number;
  opening_cents: number | null;
  market_cents: number | null;
  market_quote_count: number;
  historical_cents: number | null;
  historical: { order_id: string; rfq_id: string; rfq_title: string; total_cents: number; quantity: string; ordered_at: string } | null;
  anchor: { flagged: boolean; pct: number; basis: string; note: string };
  verified_baseline_cents: number;
  verified_basis: BaselineBasis;
  verified_savings_cents: number;
  verified_savings_pct: number;
  opening_savings_cents: number;
  reason: string;
  quote_id: string;
  snapshot: boolean;
  source: "close" | "backfill" | "live";
  computed_at: string | null;
};

export type NegotiationView = {
  rfq: { id: string; title: string; status: string; category: string; created_at: string; deadline?: string };
  strategy: Strategy | null;
  weights: Weights | null;
  rounds: Round[];
  suppliers: Supplier[];
  summary: {
    currency: string;
    initial_best_cents: number;
    current_best_cents: number;
    savings_cents: number;
    savings_pct: number;
    quote_count: number;
    counter_count: number;
    verified_savings_cents?: number;
    verified_savings_pct?: number;
    verified_basis?: BaselineBasis | "";
  };
  suggested: { target_price_cents: number; max_price_cents: number } | null;
  pause?: PauseInfo;
  baselines?: Baselines | null;
};

// Weights are the mandate's utility split; they always sum to 1.
export type Weights = { price: number; delivery: number; terms: number };

export type StrategyInput = {
  target_price_cents: number;
  max_price_cents: number;
  max_delivery_days?: number | null;
  price_weight?: number;
  max_rounds?: number;
  auto_accept?: boolean;
  tone?: Tone;
  preferred_payment_terms_days?: number | null;
  min_payment_terms_days?: number | null;
  terms_weight?: number;
};

export type NegotiationListItem = Strategy & {
  rfq_title: string;
  rfq_status: string;
  quote_count: number;
  round_count: number;
  initial_best_cents: number;
  current_best_cents: number;
  savings_cents: number;
  verified_savings_cents?: number | null;
  verified_basis?: BaselineBasis | "";
  anchored?: boolean;
};

export type VerifiedSavings = {
  currency: string;
  deals: number;
  spend_cents: number;
  baseline_cents: number;
  savings_cents: number;
  savings_pct: number;
  opening_quote_savings_cents: number;
  by_basis: Record<BaselineBasis, number>;
  anchored_deals: number;
  snapshot_deals: number;
  other_currency_deals: number;
  explanation: string;
};

export type Savings = {
  // totals and savings_pct measure against each supplier's opening quote.
  totals: { deals: number; spend_cents: number; savings_cents: number; baseline_cents: number; negotiated_deals: number };
  savings_pct: number;
  // verified is the headline: savings against the lowest fair reference.
  verified?: VerifiedSavings;
  negotiations: { active: number; awaiting_approval: number; completed: number; avg_rounds: number };
  monthly: { month: string; spend_cents: number; savings_cents: number; verified_savings_cents?: number }[];
  top_suppliers: { id: string; name: string; deals: number; spend_cents: number }[];
  recent_deals: {
    order_id: string;
    created_at: string;
    total_cents: number;
    currency: string;
    rfq_id: string;
    rfq_title: string;
    seller_name: string;
    initial_cents: number;
    rounds: number;
    verified_baseline_cents?: number;
    verified_basis?: BaselineBasis;
    verified_savings_cents?: number;
    market_cents?: number | null;
    historical_cents?: number | null;
    anchored?: boolean;
    anchor_pct?: number;
    basis_reason?: string;
    baseline_source?: string;
  }[];
};

export const BASIS_LABEL: Record<BaselineBasis, string> = {
  market: "typical market price",
  historical: "your last price",
  opening_quote: "opening quote",
  none: "no reference",
};

// basisSentence says in plain words what a verified figure is measured against.
export function basisSentence(basis: BaselineBasis | "" | undefined, b?: Pick<Baselines, "market_quote_count"> | null): string {
  switch (basis) {
    case "market":
      return `Measured against the typical opening price${b?.market_quote_count ? ` across ${b.market_quote_count} suppliers` : " across suppliers"}.`;
    case "historical":
      return "Measured against what you last paid for the same item.";
    case "opening_quote":
      return "Measured against the supplier's opening quote — the only fair reference available.";
    case "none":
      return "No reference price available, so no savings are claimed.";
    default:
      return "";
  }
}

// verifiedHeadline picks the headline figures, falling back to opening-quote
// totals for an older API that has no verified block.
export function verifiedHeadline(data: Savings): { cents: number; pct: number; currency: string; verified: boolean } {
  if (data.verified) {
    return { cents: data.verified.savings_cents, pct: data.verified.savings_pct, currency: data.verified.currency, verified: true };
  }
  return { cents: data.totals.savings_cents, pct: data.savings_pct, currency: DEFAULT_CURRENCY, verified: false };
}

async function call<T>(token: string, method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${CORE_URL}${path}`, {
    method,
    headers: {
      authorization: `Bearer ${token}`,
      ...(body !== undefined ? { "content-type": "application/json" } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    cache: "no-store",
  });
  const text = await res.text();
  if (!res.ok) throw new Error(errorMessage(text, res.status));
  return (text ? JSON.parse(text) : null) as T;
}

const base = (rfqId: string) => `/v1/rfqs/${encodeURIComponent(rfqId)}/negotiation`;

export const getNegotiation = (token: string, rfqId: string) => call<NegotiationView>(token, "GET", base(rfqId));
export const saveStrategy = (token: string, rfqId: string, input: StrategyInput) =>
  call<NegotiationView>(token, "PUT", base(rfqId), input);
export const runNegotiation = (token: string, rfqId: string) => call<NegotiationView>(token, "POST", `${base(rfqId)}/run`, {});
export const pauseNegotiation = (token: string, rfqId: string) => call<NegotiationView>(token, "POST", `${base(rfqId)}/pause`, {});
export const resumeNegotiation = (token: string, rfqId: string) => call<NegotiationView>(token, "POST", `${base(rfqId)}/resume`, {});
export const approveNegotiation = (token: string, rfqId: string, quoteId?: string) =>
  call<{ order_id: string; total_cents: number; currency: string }>(
    token,
    "POST",
    `${base(rfqId)}/approve`,
    quoteId ? { quote_id: quoteId } : {},
  );

export const declineNegotiation = (token: string, rfqId: string) =>
  call<NegotiationView>(token, "POST", `${base(rfqId)}/decline`, {});
export const listNegotiations = (token: string) =>
  call<{ negotiations: NegotiationListItem[]; pause?: PauseInfo }>(token, "GET", "/v1/negotiations");
export const getSavings = (token: string) => call<Savings>(token, "GET", "/v1/negotiations/savings");

// UAE/GCC first: amounts without a recorded currency are shown in AED.
export const DEFAULT_CURRENCY = "AED";

export function money(cents: number | null | undefined, currency = DEFAULT_CURRENCY, opts: { compact?: boolean } = {}): string {
  if (cents == null) return "—";
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: currency || DEFAULT_CURRENCY,
    notation: opts.compact ? "compact" : "standard",
    maximumFractionDigits: opts.compact ? 1 : cents % 100 === 0 ? 0 : 2,
  }).format(cents / 100);
}

export function pct(n: number, digits = 1): string {
  return `${n.toFixed(digits)}%`;
}

export const STATUS_LABEL: Record<NegotiationStatus, string> = {
  active: "Negotiating",
  paused: "Paused",
  awaiting_approval: "Needs approval",
  completed: "Deal closed",
  cancelled: "No deal",
};

export function statusClass(s: NegotiationStatus): string {
  switch (s) {
    case "active":
      return "badge badge-info";
    case "awaiting_approval":
      return "badge badge-warning";
    case "completed":
      return "badge badge-success";
    case "cancelled":
      return "badge badge-danger";
    default:
      return "badge";
  }
}

export function monthLabel(ym: string): string {
  const [y, m] = ym.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, 1)).toLocaleString("en-US", { month: "short", timeZone: "UTC" });
}

// dollarsToCents parses a user-entered amount ("12,500.50") into cents.
export function dollarsToCents(v: string): number | null {
  const n = Number(v.replace(/[,\s$]/g, ""));
  if (!Number.isFinite(n) || n <= 0) return null;
  return Math.round(n * 100);
}

const days = (n: number) => `${n} day${n === 1 ? "" : "s"}`;

// explainPick says in plain words why `pick` beats `other`, naming the
// mandate weight only when the two trade price against delivery. When the
// mandate negotiates payment terms and the two differ, it says so.
export function explainPick(pick: Supplier, other: Supplier, priceWeight: number, terms?: { weight: number }): string {
  const cur = pick.currency || DEFAULT_CURRENCY;
  const priceGap = other.price_cents - pick.price_cents;
  const dayGap =
    pick.delivery_days != null && other.delivery_days != null ? pick.delivery_days - other.delivery_days : 0;
  const price =
    priceGap > 0
      ? `${money(priceGap, cur)} cheaper than ${other.seller_name}`
      : priceGap < 0
        ? `${money(-priceGap, cur)} more than ${other.seller_name}`
        : `Same price as ${other.seller_name}`;
  const delivery = dayGap > 0 ? `${days(dayGap)} slower` : dayGap < 0 ? `${days(-dayGap)} sooner` : "";
  const tradeOff = (priceGap > 0 && dayGap > 0) || (priceGap < 0 && dayGap < 0);
  let sentence = delivery ? `${price}, ${tradeOff ? "but " : "and "}${delivery}.` : `${price}.`;
  if (terms && pick.payment_terms_days != null && other.payment_terms_days != null && pick.payment_terms_days !== other.payment_terms_days) {
    const longer = pick.payment_terms_days > other.payment_terms_days;
    sentence += ` Pays on ${paymentTermsLabel(pick.payment_terms_days).toLowerCase()} (${longer ? "more" : "less"} time to pay than ${shortTermsLabel(other.payment_terms_days).toLowerCase()}).`;
  }
  if (!tradeOff) return sentence;
  const pw = Math.round(priceWeight * 100);
  const dw = terms ? Math.round((1 - priceWeight - terms.weight) * 100) : 100 - pw;
  const favours = priceGap > 0 ? `price at ${pw}%` : `delivery at ${dw}%`;
  return `${sentence} Your mandate weighs ${favours}.`;
}

export const when = (iso: string, timeZone?: string) =>
  new Date(iso).toLocaleString("en-AE", {
    day: "numeric",
    month: "short",
    hour: "numeric",
    minute: "2-digit",
    timeZone,
  });

// How much payment terms matter, as a share of the agent's scoring. The rest
// is split between price and delivery by the priority above.
export const TERMS_IMPORTANCE = [
  { key: "low", label: "A little", weight: 0.1 },
  { key: "medium", label: "Somewhat", weight: 0.2 },
  { key: "high", label: "A lot", weight: 0.3 },
] as const;

// splitWeights turns the form's price-vs-delivery share and terms importance
// into API weights: terms first, the rest split by the priority.
export function splitWeights(priceShare: number, termsWeight: number): { price_weight: number; terms_weight: number } {
  const tw = Math.min(Math.max(termsWeight, 0), 1);
  return { price_weight: Math.round(priceShare * (1 - tw) * 1000) / 1000, terms_weight: tw };
}

// priceShareOf reverses splitWeights for a saved mandate.
export function priceShareOf(s: Pick<Strategy, "price_weight" | "terms_weight" | "preferred_payment_terms_days">): number {
  if (s.preferred_payment_terms_days == null || !s.terms_weight || s.terms_weight >= 1) return s.price_weight;
  return Math.round((s.price_weight / (1 - s.terms_weight)) * 100) / 100;
}

export const PRIORITIES = [
  { key: "price", label: "Lowest price", weight: 0.7 },
  { key: "balanced", label: "Balanced", weight: 0.5 },
  { key: "delivery", label: "Fastest delivery", weight: 0.3 },
] as const;

export type Limits = { maxCents?: number; days?: number; weight?: number; termsDays?: number };

const WORD_NUM: Record<string, number> = { a: 1, an: 1, one: 1, two: 2, three: 3, four: 4, five: 5, six: 6, ten: 10 };
const UNIT_DAYS: Record<string, number> = { day: 1, week: 7, wk: 7, month: 30 };
const DURATION = /\b(\d+|an?|one|two|three|four|five|six|ten)\s*(days?|weeks?|wks?|months?)\b/i;
const AMOUNT = /(aed\s*|dhs?\s*|dirhams?\s*)?(\d[\d,]*(?:\.\d+)?)\s*(k|thousand|m|million)?\b(\s*(?:aed|dhs?|dirhams?))?/gi;
const FAST = /\b(fast|faster|fastest|urgent|urgently|asap|quick|quickly|soon|speed|delivery (matters|first|is (more )?important))\b/i;
const CHEAP = /\b(cheap|cheaper|cheapest|lowest|best price|save|saving|price (matters|first|is (more )?important))\b/i;
const BOTH = /\b(balanced?|both)\b/i;
// Payment terms: "net 60", "60 days credit", "pay in 45 days", "cash on delivery".
const TERMS = /\bnet\s*-?\s*(\d{1,3})\b|\b(\d{1,3})\s*days?\s*(?:credit|payment terms|to pay)\b|\bpay(?:ment)?\s*(?:in|within|after)\s*(\d{1,3})\s*days?\b|\b(cash on delivery|cod)\b/i;

function parseDuration(text: string): number | undefined {
  const m = text.match(DURATION);
  if (m) {
    const n = /^\d+$/.test(m[1]) ? Number(m[1]) : WORD_NUM[m[1].toLowerCase()];
    return n * UNIT_DAYS[m[2].toLowerCase().replace(/s$/, "")];
  }
  if (/\btomorrow\b/i.test(text)) return 1;
  if (/\bnext week\b/i.test(text)) return 7;
  return undefined;
}

// parseLimits reads a budget, timeframe and priority from plain words, e.g.
// "Up to AED 50k, need it within 2 weeks, price matters most".
export function parseLimits(text: string): Limits {
  const out: Limits = {};
  const t = text.match(TERMS);
  if (t) {
    out.termsDays = t[4] ? 0 : Number(t[1] ?? t[2] ?? t[3]);
    text = text.replace(TERMS, " ");
  }
  const days = parseDuration(text);
  if (days) out.days = days;
  const rest = text.replace(new RegExp(DURATION.source, "gi"), " ");
  let amount: number | undefined;
  let marked = false;
  for (const m of rest.matchAll(AMOUNT)) {
    const mult = m[3]?.toLowerCase();
    const n = Number(m[2].replace(/,/g, "")) * (mult === "k" || mult === "thousand" ? 1000 : mult === "m" || mult === "million" ? 1_000_000 : 1);
    if (!Number.isFinite(n) || n <= 0) continue;
    if (m[1] || m[3] || m[4]) {
      if (!marked) [amount, marked] = [n, true];
    } else if (!marked && n >= 100 && (amount === undefined || n > amount)) {
      amount = n;
    }
  }
  if (amount !== undefined) out.maxCents = Math.round(amount * 100);
  const fast = FAST.test(text);
  const cheap = CHEAP.test(text);
  if (BOTH.test(text) || (fast && cheap)) out.weight = 0.5;
  else if (fast) out.weight = 0.3;
  else if (cheap) out.weight = 0.7;
  return out;
}

export function describeLimits(l: Limits, currency = DEFAULT_CURRENCY): string {
  const parts: string[] = [];
  if (l.maxCents) parts.push(`up to ${money(l.maxCents, currency)}`);
  if (l.days) parts.push(`within ${days(l.days)}`);
  const p = PRIORITIES.find((x) => x.weight === l.weight);
  if (p) parts.push(p.label.toLowerCase() + " first");
  if (l.termsDays != null) parts.push(`pay on ${paymentTermsLabel(l.termsDays).toLowerCase()}`);
  return parts.length ? `Understood: ${parts.join(" · ")}` : "";
}

// deadlineDays turns the RFQ's free-text deadline into a delivery limit in days.
export function deadlineDays(text: string, now = new Date()): number | undefined {
  const d = parseDuration(text);
  if (d) return d;
  const iso = text.match(/\b\d{4}-\d{2}-\d{2}\b/);
  if (!iso) return undefined;
  const diff = Math.ceil((Date.parse(iso[0]) - now.getTime()) / 86_400_000);
  return diff > 0 ? diff : undefined;
}

// roundDownCents mirrors the server's suggestion rounding.
export function roundDownCents(cents: number): number {
  const step = cents >= 1_000_000 ? 10_000 : cents >= 100_000 ? 1_000 : 100;
  const r = cents - (cents % step);
  return r > 0 ? r : cents;
}

export const suggestTarget = (capCents: number) => roundDownCents(Math.round(capCents * 0.88));

export function quoteRange(suppliers: Supplier[]) {
  const live = suppliers.filter((q) => q.price_cents > 0);
  if (!live.length) return null;
  const prices = live.map((q) => q.price_cents);
  const ds = live.map((q) => q.delivery_days).filter((d): d is number => d != null);
  return {
    count: live.length,
    minCents: Math.min(...prices),
    maxCents: Math.max(...prices),
    minDays: ds.length ? Math.min(...ds) : null,
    maxDays: ds.length ? Math.max(...ds) : null,
  };
}
