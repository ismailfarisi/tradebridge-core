"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { logout, useMe } from "@/lib/auth";
import type { Me } from "@/lib/api";
import NotificationBell from "@/components/NotificationBell";
import VerifyEmailBanner from "@/components/VerifyEmailBanner";
import FeedbackWidget from "@/components/FeedbackWidget";
import { IMPERSONATING_FLAG_KEY, leaveImpersonation } from "@/lib/impersonation";
import { listNegotiations } from "@/lib/negotiation";
import { Icon, type IconName } from "@/components/Icon";

type NavItem = { href: string; label: string; icon: IconName; show: (me: Me) => boolean; badge?: "approvals" };

const isSeller = (m: Me) => m.company_type === "seller" || m.company_type === "both";
const isBuyer = (m: Me) => m.company_type === "buyer" || m.company_type === "both" || !m.company_type;

const NAV: { group: string; items: NavItem[] }[] = [
  {
    group: "",
    items: [
      { href: "/dashboard", label: "Overview", icon: "home", show: () => true },
      { href: "/orders", label: "Orders", icon: "box", show: () => true },
    ],
  },
  {
    group: "Procurement",
    items: [
      { href: "/chat", label: "New RFQ", icon: "chat", show: isBuyer },
      { href: "/rfqs", label: "RFQs", icon: "list", show: isBuyer },
      { href: "/negotiations", label: "Negotiations", icon: "handshake", show: isBuyer, badge: "approvals" },
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
  {
    group: "Settings",
    items: [
      { href: "/settings/billing", label: "Billing & usage", icon: "card", show: () => true },
      { href: "/settings/team", label: "Team", icon: "users", show: () => true },
      { href: "/settings/verification", label: "Verification", icon: "shield", show: () => true },
      { href: "/account", label: "Account", icon: "user", show: () => true },
    ],
  },
];

export default function AppShell({ children }: { children: React.ReactNode }) {
  const { token, me, ready } = useMe();
  const router = useRouter();
  const pathname = usePathname();
  const [hydrated, setHydrated] = useState(false);
  const [impersonating, setImpersonating] = useState<string | null>(null);
  const [approvals, setApprovals] = useState(0);

  // On a hard page load the token store first returns its *server* snapshot
  // (null) before the client snapshot (localStorage) is read one tick later.
  // Redirecting on that first render bounces a logged-in user to /login, so we
  // wait until the client has mounted — by then the real token has been read.
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => {
    setHydrated(true);
    setImpersonating(localStorage.getItem(IMPERSONATING_FLAG_KEY));
  }, []);

  useEffect(() => {
    if (hydrated && ready && !token) router.replace("/login");
  }, [hydrated, ready, token, router]);

  useEffect(() => {
    if (!token || !me || !isBuyer(me)) return;
    listNegotiations(token)
      .then((r) => setApprovals(r.negotiations.filter((n) => n.status === "awaiting_approval").length))
      .catch(() => {});
  }, [token, me, pathname]);

  if (!hydrated || !ready) {
    return <Centered>Loading…</Centered>;
  }
  if (!me) {
    return <Centered>Redirecting…</Centered>;
  }

  const initials = (me.company_name ?? "TB")
    .split(/\s+/)
    .map((w) => w[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();

  return (
    <div className="shell">
      <aside className="side" aria-label="Main navigation">
        <Link href="/dashboard" className="brand">
          <span className="brand-mark"><Icon name="zap" size={16} strokeWidth={2.2} /></span>
          TradeBridge
        </Link>
        {NAV.map((section) => {
          const items = section.items.filter((it) => it.show(me));
          if (items.length === 0) return null;
          return (
            <div key={section.group || "main"}>
              {section.group && <div className="side-group">{section.group}</div>}
              {items.map((it) => {
                const active = pathname === it.href || pathname.startsWith(it.href + "/");
                return (
                  <Link key={it.href} href={it.href} className={`side-link${active ? " active" : ""}`} aria-current={active ? "page" : undefined}>
                    <Icon name={it.icon} />
                    {it.label}
                    {it.badge === "approvals" && approvals > 0 && <span className="count" aria-label={`${approvals} awaiting approval`}>{approvals}</span>}
                  </Link>
                );
              })}
            </div>
          );
        })}
        <div className="side-foot">
          <div style={{ color: "#fff", fontWeight: 600, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{me.company_name ?? "—"}</div>
          <div style={{ color: "#94a3b8", textTransform: "capitalize" }}>{me.company_type ?? "company"} · {me.role}</div>
        </div>
      </aside>

      <div style={{ flex: 1, display: "flex", flexDirection: "column", minWidth: 0 }}>
        <header className="topbar">
          <div className="row" style={{ gap: "0.6rem", minWidth: 0 }}>
            <span className="avatar" aria-hidden>{initials}</span>
            <strong style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{me.company_name ?? "—"}</strong>
            <span className="badge badge-info">{me.company_type ?? "company"}</span>
          </div>
          <div className="row" style={{ gap: "0.5rem" }}>
            {isBuyer(me) && (
              <Link href="/chat" className="btn btn-sm btn-primary">
                <Icon name="plus" size={16} /> New RFQ
              </Link>
            )}
            {token && <NotificationBell token={token} />}
            <button
              className="icon-btn"
              aria-label="Sign out"
              title="Sign out"
              onClick={() => {
                logout();
                router.replace("/login");
              }}
            >
              <Icon name="logout" />
            </button>
          </div>
        </header>
        {impersonating && (
          <div className="banner banner-warn" style={{ borderRadius: 0, margin: 0, justifyContent: "space-between", fontWeight: 600, fontSize: "0.85rem" }}>
            <span>Viewing as {impersonating} — read-only (support session)</span>
            <button
              className="btn btn-sm"
              onClick={() => {
                leaveImpersonation();
                router.replace("/admin/companies");
              }}
            >
              Leave
            </button>
          </div>
        )}
        {token && <VerifyEmailBanner token={token} emailVerified={me.email_verified} />}
        <main className="content">{children}</main>
        <FeedbackWidget />
      </div>
    </div>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center" }} className="muted">
      {children}
    </div>
  );
}

export function PageHeader({ title, subtitle, action }: { title: React.ReactNode; subtitle?: React.ReactNode; action?: React.ReactNode }) {
  return (
    <div className="row wrap" style={{ justifyContent: "space-between", alignItems: "flex-start", marginBottom: "1.5rem" }}>
      <div style={{ minWidth: 0 }}>
        <h1 style={{ fontSize: "1.55rem", letterSpacing: "-0.02em" }}>{title}</h1>
        {subtitle && <p className="muted" style={{ margin: "0.3rem 0 0" }}>{subtitle}</p>}
      </div>
      {action}
    </div>
  );
}
