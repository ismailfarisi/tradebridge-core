"use client";

import { req } from "./admin";

export type PlatformControl = {
  id: number;
  paused: boolean;
  reason: string;
  updated_by: string;
  updated_at: string;
};

export type KillSwitchEvent = {
  id: string;
  scope: "platform" | "company";
  company_id: string | null;
  company_name: string;
  paused: boolean;
  reason: string;
  actor: string;
  created_at: string;
};

export type KillSwitchState = { control: PlatformControl; history: KillSwitchEvent[] };

export const getKillSwitch = (token: string) =>
  req<KillSwitchState>("GET", "/admin/v1/negotiations/kill-switch", { token });

export const setKillSwitch = (token: string, paused: boolean, reason: string) =>
  req<KillSwitchState>("PUT", "/admin/v1/negotiations/kill-switch", { token, body: { paused, reason } });
