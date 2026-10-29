import { CORE_URL, errorMessage } from "./api";

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
  utility: number;
  rationale: string;
  message: string;
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
  version: number;
  status: string;
  counters: number;
  last_action: string;
  utility: number;
  recommended: boolean;
};

export type NegotiationView = {
  rfq: { id: string; title: string; status: string; category: string; created_at: string };
  strategy: Strategy | null;
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
  };
  suggested: { target_price_cents: number; max_price_cents: number } | null;
};

export type StrategyInput = {
  target_price_cents: number;
  max_price_cents: number;
  max_delivery_days?: number | null;
  price_weight?: number;
  max_rounds?: number;
  auto_accept?: boolean;
  tone?: Tone;
};

export type NegotiationListItem = Strategy & {
  rfq_title: string;
  rfq_status: string;
  quote_count: number;
  round_count: number;
  initial_best_cents: number;
  current_best_cents: number;
  savings_cents: number;
};

export type Savings = {
  totals: { deals: number; spend_cents: number; savings_cents: number; baseline_cents: number; negotiated_deals: number };
  savings_pct: number;
  negotiations: { active: number; awaiting_approval: number; completed: number; avg_rounds: number };
  monthly: { month: string; spend_cents: number; savings_cents: number }[];
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
  }[];
};

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
export const approveNegotiation = (token: string, rfqId: string) =>
  call<{ order_id: string; total_cents: number; currency: string }>(token, "POST", `${base(rfqId)}/approve`, {});
export const listNegotiations = (token: string) =>
  call<{ negotiations: NegotiationListItem[] }>(token, "GET", "/v1/negotiations");
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
