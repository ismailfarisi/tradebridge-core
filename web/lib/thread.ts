import { CORE_URL } from "./api";
import { readSSE } from "./sse";

export type ThreadMessage = {
  id: string;
  author: "buyer" | "seller";
  body: string;
  attachments: unknown;
  created_at: string;
};
export type QuoteVersion = {
  id: string;
  version: number;
  price_cents: number;
  currency: string;
  delivery_days: number | null;
  notes: string;
  source: "seller" | "counter";
  created_at: string;
};
export type Thread = {
  quote: { id: string; price_cents: number; currency: string; status: string };
  messages: ThreadMessage[];
  versions: QuoteVersion[];
};
export type ThreadEnvelope = { kind: string; quote_id?: string; message?: unknown; version?: unknown };

// Local request helper mirroring lib/orders.ts / lib/trust.ts (Fiber returns
// JSON on success, plain text on error).
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

export const getThread = (token: string, quoteId: string) =>
  req<Thread>("GET", `/v1/quotes/${quoteId}/thread`, { token });

export const postCounter = (
  token: string,
  quoteId: string,
  body: { price_cents?: number; delivery_days?: number; message: string },
) => req<{ message: ThreadMessage }>("POST", `/v1/quotes/${quoteId}/counter`, { token, body });

export const postThreadMessage = (token: string, quoteId: string, body: string) =>
  req<ThreadMessage>("POST", `/v1/quotes/${quoteId}/messages`, { token, body: { body } });

// streamThread opens the SSE (fetch + readSSE — EventSource can't send the
// Authorization header) and calls onEvent for each {kind,...} envelope.
// Returns an AbortController so the caller can close it on unmount.
export function streamThread(
  token: string,
  quoteId: string,
  onEvent: (env: ThreadEnvelope) => void,
): AbortController {
  const ctrl = new AbortController();
  fetch(`${CORE_URL}/v1/quotes/${quoteId}/thread/stream`, {
    headers: { authorization: `Bearer ${token}` },
    signal: ctrl.signal,
    cache: "no-store",
  })
    .then((res) => readSSE(res, (_event, data) => {
      try {
        onEvent(JSON.parse(data) as ThreadEnvelope);
      } catch {
        /* ignore malformed frames */
      }
    }))
    .catch(() => {}); // aborted / network — caller re-fetches on remount
  return ctrl;
}
