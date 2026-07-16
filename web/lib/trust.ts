import { CORE_URL } from "./api";

export type VerificationStatus = "unverified" | "pending" | "verified" | "rejected";

export type PendingClaim = {
  domain: string;
  record_name: string;
  record_type: string;
  record_value: string;
};
export type VerificationState = {
  verification_status: VerificationStatus;
  verified_domain: string | null;
  verified_at: string | null;
  pending: PendingClaim[];
};
export type Challenge = {
  domain: string;
  record_name: string;
  record_type: string;
  record_value: string;
};
export type CheckResult = { status: string; verified: boolean };
export type CatalogShowcaseItem = { name: string; category: string; summary: string | null };
export type PublicSeller = {
  company_id: string;
  name: string;
  verification_status: VerificationStatus;
  verified_domain: string | null;
  description: string | null;
  categories: string[];
  location: string | null;
  catalog: CatalogShowcaseItem[];
  reputation: null;
};
export type BlockRow = {
  id: string;
  blocked_company_id: string;
  blocked_name: string;
  created_at: string;
};

// Local request helper mirroring lib/billing.ts (Fiber returns JSON on success,
// plain text on error).
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
  return (text ? JSON.parse(text) : undefined) as T;
}

export const getVerification = (token: string) =>
  call<VerificationState>("GET", "/v1/verification", { token });

export const claimDomain = (token: string, domain: string) =>
  call<Challenge>("POST", "/v1/verification/domain", { token, body: { domain } });

export const checkDomain = (token: string, domain: string) =>
  call<CheckResult>("POST", "/v1/verification/domain/check", { token, body: { domain } });

export const getPublicSeller = (companyId: string) =>
  call<PublicSeller>("GET", `/public/v1/sellers/${companyId}`);

export const createReport = (token: string, reportedCompanyId: string, reason: string, detail: string) =>
  call<{ id: string }>("POST", "/v1/reports", {
    token,
    body: { reported_company_id: reportedCompanyId, reason, detail },
  });

export const listBlocks = (token: string) =>
  call<{ blocks: BlockRow[]; count: number }>("GET", "/v1/blocks", { token }).then((r) => r.blocks);

export const createBlock = (token: string, blockedCompanyId: string) =>
  call<{ status: string }>("POST", "/v1/blocks", { token, body: { blocked_company_id: blockedCompanyId } });

export const removeBlock = (token: string, blockedCompanyId: string) =>
  call<null>("DELETE", `/v1/blocks/${blockedCompanyId}`, { token });
