// Thin client for the Core API. In Phase 0 it only pings /health so the
// landing page can show a live backend status.

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
