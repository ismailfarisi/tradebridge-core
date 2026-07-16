import { CORE_URL } from "./api";

export type Notification = {
  id: string;
  type: string;
  title: string;
  body: string;
  data?: Record<string, unknown>;
  read: boolean;
  created_at: string;
};
export type NotificationList = { notifications: Notification[]; unread: number };

// Local request helper (lib/api.ts's req() is not exported). Mirrors its
// semantics: JSON on success, plain-text error → throw; auth header optional.
async function call<T>(
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
  // Unlike api.ts's req() (which returns null for an empty body), this
  // returns undefined — all void-typed calls here (mark-read, etc.) should
  // resolve to undefined, not null.
  return (text ? JSON.parse(text) : undefined) as T;
}

export function listNotifications(token: string, opts: { limit?: number; offset?: number } = {}): Promise<NotificationList> {
  const qs = new URLSearchParams();
  if (opts.limit != null) qs.set("limit", String(opts.limit));
  if (opts.offset != null) qs.set("offset", String(opts.offset));
  const q = qs.toString();
  return call<NotificationList>("GET", `/v1/notifications${q ? `?${q}` : ""}`, { token });
}

export function markNotificationRead(token: string, id: string): Promise<void> {
  return call<void>("POST", `/v1/notifications/${id}/read`, { token });
}

export function markAllNotificationsRead(token: string): Promise<void> {
  return call<void>("POST", `/v1/notifications/read-all`, { token });
}

export function resendVerification(token: string): Promise<void> {
  return call<void>("POST", `/v1/auth/resend-verification`, { token });
}

// Consumes the EMAIL verification token (public route, no auth header).
export function verifyEmail(emailToken: string): Promise<void> {
  return call<void>("POST", `/v1/auth/verify-email`, { body: { token: emailToken } });
}

export function joinWaitlist(email: string, source?: string): Promise<void> {
  return call<void>("POST", `/v1/waitlist`, { body: { email, ...(source ? { source } : {}) } });
}
