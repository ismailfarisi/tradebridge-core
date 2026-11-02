import type { Me } from "@/lib/api";
import type { IconName } from "@/components/Icon";

export type NavItem = { href: string; label: string; short?: string; icon: IconName; show: (me: Me) => boolean; badge?: "needsYou" };
export type NavSection = { group: string; items: NavItem[] };

export const isSeller = (m: Me) => m.company_type === "seller" || m.company_type === "both";
export const isBuyer = (m: Me) => m.company_type === "buyer" || m.company_type === "both" || !m.company_type;

// Ordered the way a deal moves: request, negotiate, order, then the savings it produced.
export const NAV: NavSection[] = [
  {
    group: "",
    items: [
      { href: "/dashboard", label: "Overview", icon: "home", show: () => true },
      { href: "/rfqs", label: "RFQs", icon: "list", show: isBuyer },
      { href: "/negotiations", label: "Negotiations", short: "Deals", icon: "handshake", show: isBuyer, badge: "needsYou" },
      { href: "/orders", label: "Orders", icon: "box", show: () => true },
      { href: "/savings", label: "Savings", icon: "trending", show: isBuyer },
    ],
  },
  {
    group: "Selling",
    items: [
      { href: "/seller/profile", label: "Supplier profile", icon: "store", show: isSeller },
      { href: "/seller/catalog", label: "Catalog", icon: "layers", show: isSeller },
      { href: "/seller/agent", label: "Sales agent", icon: "bot", show: isSeller },
    ],
  },
];

export const ACCOUNT_LINKS: { href: string; label: string; icon: IconName }[] = [
  { href: "/account", label: "Account", icon: "user" },
  { href: "/settings/team", label: "Team", icon: "users" },
  { href: "/settings/billing", label: "Billing & usage", icon: "card" },
  { href: "/settings/verification", label: "Verification", icon: "shield" },
];

export const visibleNav = (me: Me): NavSection[] =>
  NAV.map((s) => ({ ...s, items: s.items.filter((it) => it.show(me)) })).filter((s) => s.items.length > 0);

// The negotiation room lives under /rfqs/:id but is reached from Negotiations.
const navHref = (pathname: string) => (/^\/rfqs\/[^/]+\/negotiation$/.test(pathname) ? "/negotiations" : pathname);

export const isActive = (pathname: string, href: string) => {
  const p = navHref(pathname);
  return p === href || p.startsWith(href + "/");
};

const TITLES: Record<string, string> = {
  "/dashboard": "Overview",
  "/rfqs": "RFQs",
  "/negotiations": "Negotiations",
  "/orders": "Orders",
  "/savings": "Savings",
  "/account": "Account",
  "/settings/team": "Team",
  "/settings/billing": "Billing & usage",
  "/settings/verification": "Verification",
  "/seller/profile": "Supplier profile",
  "/seller/catalog": "Catalog",
  "/seller/agent": "Sales agent",
};

export type Crumb = { title: string; back?: { href: string; label: string } };

export function pageCrumb(pathname: string): Crumb {
  const p = pathname.length > 1 ? pathname.replace(/\/$/, "") : pathname;
  if (TITLES[p]) return { title: TITLES[p] };
  if (/^\/rfqs\/[^/]+\/negotiation$/.test(p)) return { title: "Negotiation", back: { href: "/negotiations", label: "Negotiations" } };
  if (/^\/rfqs\/[^/]+$/.test(p)) return { title: "RFQ details", back: { href: "/rfqs", label: "RFQs" } };
  if (/^\/orders\/[^/]+$/.test(p)) return { title: "Order details", back: { href: "/orders", label: "Orders" } };
  return { title: "" };
}
