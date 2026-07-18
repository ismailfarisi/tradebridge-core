"use client";

import { CORE_URL } from "./api";
import type { Dispute } from "./orders";
import { useSyncExternalStore } from "react";

const ADMIN_TOKEN_KEY = "tb_admin_token";
const listeners = new Set<() => void>();
function emit() { listeners.forEach((l) => l()); }
export function subscribeAdminToken(cb: () => void) { listeners.add(cb); return () => listeners.delete(cb); }
export function adminTokenSnapshot(): string | null {
  return typeof window !== "undefined" ? localStorage.getItem(ADMIN_TOKEN_KEY) : null;
}
export function setAdminToken(t: string) { localStorage.setItem(ADMIN_TOKEN_KEY, t); emit(); }
export function clearAdminToken() { localStorage.removeItem(ADMIN_TOKEN_KEY); emit(); }
export function useAdminToken(): string | null {
  return useSyncExternalStore(subscribeAdminToken, adminTokenSnapshot, () => null);
}

export async function req<T>(method: string, path: string, opts: { token?: string | null; body?: unknown } = {}): Promise<T> {
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

export type AdminSettings = {
  extraction: { provider: string; model: string; base_url: string; has_key: boolean; key_last4: string };
  extraction_system_prompt: string;
  intake_system_prompt: string;
  default_extraction_prompt: string;
  default_intake_prompt: string;
};
export type AdminSettingsInput = {
  provider: string; model: string; base_url: string;
  api_key?: string; clear_api_key?: boolean;
  extraction_system_prompt: string; intake_system_prompt: string;
};

export type TestResult = { ok: boolean; model: string; latency_ms: number; error: string };
export type LlmHealth = {
  window: number;
  recent_fallbacks: number;
  last_error: string;
  last_fallback_at: string | null;
  degraded: boolean;
};

export const adminLogin = (email: string, password: string) =>
  req<{ token: string }>("POST", "/admin/v1/login", { body: { email, password } });
export const adminMe = (token: string) =>
  req<{ admin_id: string; email: string }>("GET", "/admin/v1/me", { token });
export const getAdminSettings = (token: string) =>
  req<AdminSettings>("GET", "/admin/v1/settings/", { token });
export const updateAdminSettings = (token: string, body: AdminSettingsInput) =>
  req<AdminSettings>("PUT", "/admin/v1/settings/", { token, body });
export const testAdminSettings = (token: string) =>
  req<TestResult>("POST", "/admin/v1/settings/test", { token });
export const getAdminHealth = (token: string) =>
  req<LlmHealth>("GET", "/admin/v1/settings/health", { token });

export const listDisputes = (token: string, status = "") =>
  req<{ disputes: Dispute[] }>("GET", `/admin/v1/disputes${status ? `?status=${status}` : ""}`, { token });
export const resolveDispute = (token: string, id: string, outcome: string, orderTo: string, resolution: string) =>
  req<{ dispute: Dispute }>("POST", `/admin/v1/disputes/${id}/resolve`, {
    token,
    body: { outcome, order_to: orderTo, resolution },
  });

// --- companies (Sprint 8: unified admin console) ---

export type AdminCompany = { id: string; name: string; type: string; status: string; created_at: string };
export type AdminCompanyDetail = AdminCompany & {
  verification_status: string;
  plan_code: string;
  users: { id: string; email: string; role: string }[];
  counts: { rfqs: number; quotes: number; orders: number };
};
export type VerificationCompany = {
  id: string;
  name: string;
  type: string;
  verification_status: string;
  verified_domain: string | null;
  verified_at: string | null;
};
export type AdminReport = {
  id: string;
  reporter_company_id: string;
  reported_company_id: string;
  reason: string;
  detail: string | null;
  status: string;
  created_at: string;
  resolved_at: string | null;
  reporter_name: string;
  reported_name: string;
};
export type AuditEntry = { id: string; company_id: string; actor: string; action: string; target_id: string | null; detail: unknown; created_at: string };

// adminQuery builds a query string from defined, non-empty params only.
export function adminQuery(params: Record<string, string | undefined>): string {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v) p.set(k, v);
  }
  return p.toString();
}

export const listCompanies = (token: string, q = "", status = "") =>
  req<{ companies: AdminCompany[]; count: number }>("GET", `/admin/v1/companies/?${adminQuery({ q, status })}`, { token });
export const getCompany = (token: string, id: string) =>
  req<AdminCompanyDetail>("GET", `/admin/v1/companies/${id}`, { token });
export const suspendCompany = (token: string, id: string, reason: string) =>
  req<{ company_id: string; status: string }>("POST", `/admin/v1/companies/${id}/suspend`, { token, body: { reason } });
export const unsuspendCompany = (token: string, id: string) =>
  req<{ company_id: string; status: string }>("POST", `/admin/v1/companies/${id}/unsuspend`, { token });

export const listVerifications = (token: string, status = "pending") =>
  req<{ companies: VerificationCompany[]; count: number }>("GET", `/admin/v1/verifications?${adminQuery({ status })}`, { token });
export const resolveVerification = (token: string, id: string, status: string, reason = "") =>
  req<{ company_id: string; verification_status: string }>("POST", `/admin/v1/companies/${id}/verification`, { token, body: { status, reason } });

export const listReports = (token: string, status = "open") =>
  req<{ reports: AdminReport[]; count: number }>("GET", `/admin/v1/reports?${adminQuery({ status })}`, { token });
export const resolveReport = (token: string, id: string, status: string) =>
  req<AdminReport>("POST", `/admin/v1/reports/${id}`, { token, body: { status } });

export const listAudit = (token: string, filters: { company_id?: string; actor?: string; action?: string } = {}) =>
  req<{ entries: AuditEntry[]; count: number }>("GET", `/admin/v1/audit?${adminQuery(filters)}`, { token });

export const impersonate = (token: string, companyId: string) =>
  req<{ token: string; company_id: string; user_id: string; expires_in: number }>("POST", "/admin/v1/impersonate", { token, body: { company_id: companyId } });
