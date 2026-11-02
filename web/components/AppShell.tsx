"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { logout, useMe } from "@/lib/auth";
import NotificationBell from "@/components/NotificationBell";
import VerifyEmailBanner from "@/components/VerifyEmailBanner";
import FeedbackWidget from "@/components/FeedbackWidget";
import { IMPERSONATING_FLAG_KEY, leaveImpersonation } from "@/lib/impersonation";
import { listNegotiations } from "@/lib/negotiation";
import { listRfqs } from "@/lib/api";
import { Icon } from "@/components/Icon";
import { needsYou } from "@/components/NeedsYou";
import AccountMenu from "@/components/AccountMenu";
import MobileNav from "@/components/MobileNav";
import { isActive, isBuyer, pageCrumb, visibleNav } from "@/lib/nav";

export default function AppShell({ children }: { children: React.ReactNode }) {
  const { token, me, ready } = useMe();
  const router = useRouter();
  const pathname = usePathname();
  const [hydrated, setHydrated] = useState(false);
  const [impersonating, setImpersonating] = useState<string | null>(null);
  const [decisions, setDecisions] = useState(0);

  // On a hard page load the token store first returns its *server* snapshot
  // (null) before the client snapshot (localStorage) is read one tick later.
  // Redirecting on that first render bounces a logged-in user to /login, so we
  // wait until the client has mounted — by then the real token has been read.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setHydrated(true);
    setImpersonating(localStorage.getItem(IMPERSONATING_FLAG_KEY));
  }, []);

  useEffect(() => {
    if (hydrated && ready && !token) router.replace("/login");
  }, [hydrated, ready, token, router]);

  useEffect(() => {
    if (!token || !me || !isBuyer(me)) return;
    Promise.all([listNegotiations(token), listRfqs(token)])
      .then(([n, r]) => {
        const todo = needsYou(n.negotiations, r.rfqs);
        setDecisions(todo.approvals.length + todo.unmanaged.length);
      })
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

  const nav = visibleNav(me);
  const crumb = pageCrumb(pathname);
  const signOut = () => {
    logout();
    router.replace("/login");
  };

  return (
    <div className="shell">
      <aside className="side" aria-label="Main navigation">
        <Link href="/dashboard" className="brand">
          <span className="brand-mark"><Icon name="zap" size={16} strokeWidth={2.2} /></span>
          TradeBridge
        </Link>
        {nav.map((section) => (
          <div key={section.group || "main"}>
            {section.group && <div className="side-group">{section.group}</div>}
            {section.items.map((it) => {
              const active = isActive(pathname, it.href);
              const count = it.badge === "needsYou" ? decisions : 0;
              return (
                <Link key={it.href} href={it.href} className={`side-link${active ? " active" : ""}`} aria-current={active ? "page" : undefined}>
                  <Icon name={it.icon} />
                  {it.label}
                  {count > 0 && (
                    <>
                      <span className="count" aria-hidden>{count}</span>
                      <span className="sr-only">, {count} need you</span>
                    </>
                  )}
                </Link>
              );
            })}
          </div>
        ))}
        <div className="side-foot">
          <div className="truncate" style={{ color: "#fff", fontWeight: 600 }}>{me.company_name ?? "—"}</div>
          <div style={{ color: "#94a3b8", textTransform: "capitalize" }}>{me.company_type ?? "company"} · {me.role}</div>
        </div>
      </aside>

      <div style={{ flex: 1, display: "flex", flexDirection: "column", minWidth: 0 }}>
        <header className="topbar">
          <div className="row" style={{ gap: "0.6rem", minWidth: 0 }}>
            <Link href="/dashboard" className="brand-mark topbar-brand" aria-label="TradeBridge overview">
              <Icon name="zap" size={16} strokeWidth={2.2} />
            </Link>
            <nav aria-label="Breadcrumb" className={`crumb${crumb.back ? " crumb-has-back" : ""}`}>
              {crumb.back && (
                <>
                  <Link href={crumb.back.href} className="crumb-back">
                    <Icon name="arrowLeft" size={16} /> {crumb.back.label}
                  </Link>
                  <span className="crumb-sep" aria-hidden>/</span>
                </>
              )}
              {crumb.title && <span className="crumb-cur" aria-current="page">{crumb.title}</span>}
            </nav>
          </div>
          <div className="row" style={{ gap: "0.5rem", flexShrink: 0 }}>
            {isBuyer(me) && (
              <Link href="/chat" className="btn btn-sm btn-primary" aria-label="New RFQ">
                <Icon name="plus" size={16} /> <span className="topbar-label">New RFQ</span>
              </Link>
            )}
            {token && <NotificationBell token={token} />}
            <AccountMenu me={me} initials={initials} onSignOut={signOut} />
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
      <MobileNav me={me} items={nav.flatMap((s) => s.items)} pathname={pathname} decisions={decisions} onSignOut={signOut} />
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
