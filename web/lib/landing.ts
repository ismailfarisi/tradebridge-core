import type { Me } from "./api";

// Buyers (buyer/both/unset) land in the chat; seller-only companies keep the
// classic dashboard/AppShell.
export function landingPath(me: Me): string {
  return me.company_type === "seller" ? "/dashboard" : "/chat";
}
