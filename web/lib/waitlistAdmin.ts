"use client";

import { req, adminQuery } from "./admin";

export type WaitlistSignup = {
  id: string;
  email: string;
  source: string | null;
  created_at: string;
  invited_at: string | null;
};

export type WaitlistStatus = "pending" | "invited" | "";

export const listWaitlist = (token: string, status: WaitlistStatus = "") =>
  req<{ signups: WaitlistSignup[]; count: number }>(
    "GET",
    `/admin/v1/waitlist/?${adminQuery({ status })}`,
    { token },
  );

export const inviteWaitlist = (token: string, ids?: string[]) =>
  req<{ sent: number; attempted: number }>("POST", "/admin/v1/waitlist/invite", {
    token,
    body: ids && ids.length > 0 ? { ids } : {},
  });
