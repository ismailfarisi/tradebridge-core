"use client";

import { req } from "./admin";

export type FeedbackRow = {
  id: string;
  company_id: string;
  user_id: string;
  kind: "feedback" | "nps";
  score: number | null;
  message: string | null;
  created_at: string;
};

export const listFeedback = (token: string) =>
  req<{ feedback: FeedbackRow[]; count: number }>("GET", "/admin/v1/feedback", { token });
