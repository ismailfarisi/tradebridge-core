// Thin client for the Core API: health, auth (to obtain a token), and the
// per-company provider settings surface.

export const CORE_URL =
  process.env.NEXT_PUBLIC_CORE_URL ?? "http://localhost:8080";

export type HealthState = "ok" | "unreachable";

export async function fetchHealth(): Promise<HealthState> {
  try {
    const res = await fetch(`${CORE_URL}/health`, { cache: "no-store" });
    if (!res.ok) return "unreachable";
    const body = (await res.json()) as { status?: string };
    return body.status === "ok" ? "ok" : "unreachable";
  } catch {
    return "unreachable";
  }
}

// --- auth (token stored in localStorage) ---

const TOKEN_KEY = "tb_core_token";

const tokenListeners = new Set<() => void>();
function emitToken() {
  tokenListeners.forEach((l) => l());
}

// useSyncExternalStore wiring — SSR-safe token reads with no effect.
export function subscribeToken(cb: () => void): () => void {
  tokenListeners.add(cb);
  return () => tokenListeners.delete(cb);
}
export function tokenSnapshot(): string | null {
  return typeof window !== "undefined" ? localStorage.getItem(TOKEN_KEY) : null;
}
export function tokenServerSnapshot(): string | null {
  return null;
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
  emitToken();
}
export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
  emitToken();
}

// The Core API returns fiber errors as plain text.
async function errorText(res: Response, fallback: string): Promise<string> {
  const t = await res.text().catch(() => "");
  return t || fallback;
}

export async function login(email: string, password: string): Promise<string> {
  const res = await fetch(`${CORE_URL}/v1/auth/login`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) throw new Error(await errorText(res, "login failed"));
  const body = (await res.json()) as { token: string };
  return body.token;
}

// --- extraction settings ---

export type ExtractionSettings = {
  provider: string;
  model: string;
  base_url: string;
  has_key: boolean;
  key_last4: string;
};

export type ExtractionUpdate = {
  provider: string;
  model: string;
  base_url: string;
  api_key?: string;
  clear_api_key?: boolean;
};

export async function getExtractionSettings(
  token: string,
): Promise<ExtractionSettings> {
  const res = await fetch(`${CORE_URL}/v1/settings/extraction`, {
    headers: { authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  if (!res.ok) throw new Error(await errorText(res, "failed to load settings"));
  return res.json();
}

export async function updateExtractionSettings(
  token: string,
  body: ExtractionUpdate,
): Promise<ExtractionSettings> {
  const res = await fetch(`${CORE_URL}/v1/settings/extraction`, {
    method: "PUT",
    headers: {
      authorization: `Bearer ${token}`,
      "content-type": "application/json",
    },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(await errorText(res, "save failed"));
  return res.json();
}
