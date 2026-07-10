"use client";

import { useEffect, useState } from "react";
import { health } from "./api";

// useCoreHealth polls Core /health so the app bar can show reachability.
// Returns null until the first probe settles, then true/false.
export function useCoreHealth(intervalMs = 10000): boolean | null {
  const [ok, setOk] = useState<boolean | null>(null);
  useEffect(() => {
    let active = true;
    const probe = () =>
      health()
        .then(() => active && setOk(true))
        .catch(() => active && setOk(false));
    probe();
    const t = setInterval(probe, intervalMs);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, [intervalMs]);
  return ok;
}
