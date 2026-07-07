"use client";

import { useEffect, useState, useSyncExternalStore } from "react";
import {
  clearToken,
  me as fetchMe,
  subscribeToken,
  tokenServerSnapshot,
  tokenSnapshot,
  type Me,
} from "./api";

export function useToken(): string | null {
  return useSyncExternalStore(subscribeToken, tokenSnapshot, tokenServerSnapshot);
}

/**
 * Resolves the current user from the stored token. `ready` is true once the
 * fetch settles (or immediately when logged out). setState only happens in the
 * async callbacks, keeping this SSR-safe and lint-clean.
 */
export function useMe(): { token: string | null; me: Me | null; ready: boolean } {
  const token = useToken();
  const [profile, setProfile] = useState<Me | null>(null);
  const [fetchedFor, setFetchedFor] = useState<string | null>(null);

  useEffect(() => {
    if (!token) return;
    let active = true;
    fetchMe(token)
      .then((m) => {
        if (active) {
          setProfile(m);
          setFetchedFor(token);
        }
      })
      .catch(() => {
        if (active) clearToken();
      });
    return () => {
      active = false;
    };
  }, [token]);

  const ready = !token || fetchedFor === token;
  return { token, me: token ? profile : null, ready };
}

export function logout() {
  clearToken();
}
