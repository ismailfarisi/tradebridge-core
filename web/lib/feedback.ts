"use client";

import { CORE_URL } from "./api";

export type FeedbackInput = { kind: "feedback"; message: string } | { kind: "nps"; score: number };

export async function sendFeedback(token: string, input: FeedbackInput): Promise<void> {
  const res = await fetch(`${CORE_URL}/v1/feedback`, {
    method: "POST",
    headers: { "content-type": "application/json", authorization: `Bearer ${token}` },
    body: JSON.stringify(input),
  });
  if (!res.ok) throw new Error(await res.text());
}

export type NpsCategory = "promoter" | "passive" | "detractor";

export function npsCategory(score: number): NpsCategory {
  if (score >= 9) return "promoter";
  if (score >= 7) return "passive";
  return "detractor";
}

// npsScore: % promoters − % detractors over nps rows, rounded; 0 when no rows.
export function npsScore(scores: number[]): number {
  if (scores.length === 0) return 0;
  const p = scores.filter((s) => s >= 9).length;
  const d = scores.filter((s) => s <= 6).length;
  return Math.round(((p - d) / scores.length) * 100);
}
