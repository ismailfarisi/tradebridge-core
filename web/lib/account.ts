"use client";

import { CORE_URL } from "./api";

export async function exportData(token: string): Promise<unknown> {
  const res = await fetch(`${CORE_URL}/v1/account/export`, {
    headers: { authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function deleteAccount(token: string, password: string): Promise<void> {
  const res = await fetch(`${CORE_URL}/v1/account/delete`, {
    method: "POST",
    headers: { "content-type": "application/json", authorization: `Bearer ${token}` },
    body: JSON.stringify({ password }),
  });
  if (!res.ok) throw new Error(await res.text());
}

// canDelete gates the delete UI to owners.
export function canDelete(role: string): boolean {
  return role === "owner";
}
