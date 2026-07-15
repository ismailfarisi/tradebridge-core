// Client for the Core API. Fiber returns JSON on success and plain-text on
// error, so the request helper handles both.

import { readSSE } from "./sse";

export const CORE_URL =
  process.env.NEXT_PUBLIC_CORE_URL ?? "http://localhost:8080";

// --- token store (localStorage + useSyncExternalStore) ---

const TOKEN_KEY = "tb_core_token";
const listeners = new Set<() => void>();
function emit() {
  listeners.forEach((l) => l());
}
export function subscribeToken(cb: () => void): () => void {
  listeners.add(cb);
  return () => listeners.delete(cb);
}
export function tokenSnapshot(): string | null {
  return typeof window !== "undefined" ? localStorage.getItem(TOKEN_KEY) : null;
}
export function tokenServerSnapshot(): string | null {
  return null;
}
export function setToken(t: string): void {
  localStorage.setItem(TOKEN_KEY, t);
  emit();
}
export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
  emit();
}

async function req<T>(
  method: string,
  path: string,
  opts: { token?: string | null; body?: unknown } = {},
): Promise<T> {
  const res = await fetch(`${CORE_URL}${path}`, {
    method,
    headers: {
      ...(opts.body ? { "content-type": "application/json" } : {}),
      ...(opts.token ? { authorization: `Bearer ${opts.token}` } : {}),
    },
    body: opts.body ? JSON.stringify(opts.body) : undefined,
    cache: "no-store",
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return (text ? JSON.parse(text) : null) as T;
}

// --- types ---

export type CompanyType = "buyer" | "seller" | "both";
export type Me = {
  company_id: string;
  user_id: string;
  role: string;
  company_name?: string;
  company_type?: CompanyType;
};
export type RegisterInput = {
  company_name: string;
  company_type: CompanyType;
  email: string;
  password: string;
};
export type RFQ = {
  id: string;
  title: string;
  raw_text: string;
  requirements: Record<string, unknown>;
  category: string;
  status: string;
  match_count: number;
  messages?: ChatMessage[];
  created_at: string;
};
export type AttachmentMeta = {
  id: string;
  filename: string;
  mime: string;
  size_bytes: number;
};
export type ChatMessage = {
  role: "user" | "assistant";
  content: string;
  attachment_ids?: string[];
  attachments?: AttachmentMeta[];
};
export type QuoteMessage = {
  id: string;
  author: "buyer" | "seller";
  body: string;
  attachments: AttachmentMeta[] | null;
  created_at: string;
};
export type QuoteRow = {
  id: string;
  seller_company_id: string;
  seller_name?: string;
  price_cents: number;
  currency: string;
  delivery_days: number | null;
  notes: string;
  status: string;
  rank?: number;
};
export type CatalogItem = {
  id: string;
  name: string;
  category: string;
  summary: string | null;
};
export type AgentCred = {
  id: string;
  label: string;
  status: string;
  last_seen: unknown;
};
// --- auth ---

export const health = () => req<{ status: string }>("GET", "/health");
export const register = (b: RegisterInput) =>
  req<{ token: string; role: string; company_id: string }>("POST", "/v1/auth/register", { body: b });
export const login = (email: string, password: string) =>
  req<{ token: string }>("POST", "/v1/auth/login", { body: { email, password } });
export const me = (token: string) => req<Me>("GET", "/v1/me", { token });

// --- buyer: RFQs ---

export const createRfq = (token: string, title: string, text: string) =>
  req<{ rfq: RFQ; match_count: number }>("POST", "/v1/rfqs", { token, body: { title, text } });
export const listRfqs = (token: string) =>
  req<{ rfqs: RFQ[]; count: number }>("GET", "/v1/rfqs", { token });
export const getRfq = (token: string, id: string) =>
  req<{ rfq: RFQ; match_count: number; matches: unknown[] }>("GET", `/v1/rfqs/${id}`, { token });
export const rfqQuotes = (token: string, id: string) =>
  req<{ quotes: QuoteRow[]; count: number }>("GET", `/v1/rfqs/${id}/quotes`, { token });
export const acceptQuote = (token: string, quoteId: string) =>
  req<{ order_id: string; total_cents: number }>("POST", `/v1/quotes/${quoteId}/accept`, { token, body: {} });

// Multipart upload — bypasses req() because that wrapper is JSON-only.
export async function uploadAttachment(token: string, file: File): Promise<AttachmentMeta> {
  const form = new FormData();
  form.append("file", file, file.name);
  const res = await fetch(`${CORE_URL}/v1/attachments`, {
    method: "POST",
    headers: { authorization: `Bearer ${token}` },
    body: form,
    cache: "no-store",
  });
  if (!res.ok) throw new Error((await res.text()) || `upload failed (${res.status})`);
  return res.json();
}

// Downloads need the auth header, so <img src> can't hit the URL directly —
// fetch a Blob and object-URL it in the component.
export async function fetchAttachmentBlob(token: string, id: string): Promise<Blob> {
  const res = await fetch(`${CORE_URL}/v1/attachments/${id}`, {
    headers: { authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  if (!res.ok) throw new Error(`attachment fetch failed (${res.status})`);
  return res.blob();
}

export const quoteMessages = (token: string, quoteId: string) =>
  req<{ messages: QuoteMessage[]; count: number }>("GET", `/v1/quotes/${quoteId}/messages`, { token });

export const postQuoteMessage = (token: string, quoteId: string, body: string, attachmentIds: string[]) =>
  req<QuoteMessage>("POST", `/v1/quotes/${quoteId}/messages`, {
    token,
    body: { body, attachment_ids: attachmentIds },
  });
export const closeRfq = (token: string, id: string) =>
  req<RFQ>("POST", `/v1/rfqs/${id}/close`, { token, body: {} });
export const rfqChat = (token: string, messages: ChatMessage[]) =>
  req<{ done: boolean; assistant: string; rfq?: RFQ; match_count?: number }>(
    "POST",
    "/v1/rfq-chat",
    { token, body: { messages } },
  );

export type RfqChatDone = { done: boolean; assistant: string; rfq?: RFQ; match_count?: number };

/** Streaming variant of rfqChat: onDelta receives assistant-text fragments;
 *  the resolved value is authoritative and replaces the streamed text. */
export async function rfqChatStream(
  token: string,
  messages: ChatMessage[],
  onDelta: (text: string) => void,
): Promise<RfqChatDone> {
  const res = await fetch(`${CORE_URL}/v1/rfq-chat/stream`, {
    method: "POST",
    headers: { "content-type": "application/json", authorization: `Bearer ${token}` },
    body: JSON.stringify({ messages }),
    cache: "no-store",
  });
  if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`);
  let result: RfqChatDone | null = null;
  let errMsg: string | null = null;
  await readSSE(res, (event, data) => {
    if (event === "delta") onDelta((JSON.parse(data) as { text: string }).text);
    else if (event === "done") result = JSON.parse(data) as RfqChatDone;
    else if (event === "error") errMsg = (JSON.parse(data) as { message: string }).message;
  });
  if (errMsg) throw new Error(errMsg);
  if (!result) throw new Error("stream ended without a result");
  return result;
}

// --- seller ---

export const getProfile = (token: string) =>
  req<Record<string, unknown>>("GET", "/v1/suppliers/me", { token }).catch(() => null);
export const upsertProfile = (token: string, body: { description: string; categories: string[]; location: string }) =>
  req<Record<string, unknown>>("PUT", "/v1/suppliers/me", { token, body });
export const listCatalog = (token: string) =>
  req<{ items: CatalogItem[]; count: number }>("GET", "/v1/catalog", { token });
export const createCatalog = (token: string, body: { name: string; category: string; summary: string }) =>
  req<CatalogItem>("POST", "/v1/catalog", { token, body });
export const deleteCatalog = (token: string, id: string) =>
  req<null>("DELETE", `/v1/catalog/${id}`, { token });
export const listAgentCreds = (token: string) =>
  req<{ credentials: AgentCred[] }>("GET", "/v1/agent-credentials", { token });
export const issueAgentCred = (token: string, label: string) =>
  req<{ id: string; label: string; secret: string }>("POST", "/v1/agent-credentials", { token, body: { label } });
export const revokeAgentCred = (token: string, id: string) =>
  req<null>("POST", `/v1/agent-credentials/${id}/revoke`, { token, body: {} });
