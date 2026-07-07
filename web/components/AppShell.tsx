"use client";

import { useEffect } from "react";
import { usePathname, useRouter } from "next/navigation";
import { logout, useMe } from "@/lib/auth";
import type { Me } from "@/lib/api";

type NavItem = { href: string; label: string; show: (me: Me) => boolean };

const isSeller = (m: Me) => m.company_type === "seller" || m.company_type === "both";
const isBuyer = (m: Me) => m.company_type === "buyer" || m.company_type === "both" || !m.company_type;
const isAdmin = (m: Me) => m.role === "owner" || m.role === "admin";

const NAV: { group: string; items: NavItem[] }[] = [
  {
    group: "",
    items: [{ href: "/dashboard", label: "Overview", show: () => true }],
  },
  {
    group: "Buyer",
    items: [{ href: "/rfqs", label: "RFQs", show: isBuyer }],
  },
  {
    group: "Seller",
    items: [
      { href: "/seller/profile", label: "Supplier profile", show: isSeller },
      { href: "/seller/catalog", label: "Catalog", show: isSeller },
      { href: "/seller/agent", label: "Agent credentials", show: isSeller },
    ],
  },
  {
    group: "Admin",
    items: [
      { href: "/admin/models", label: "Model settings", show: isAdmin },
      { href: "/admin/usage", label: "Usage", show: isAdmin },
      { href: "/admin/fraud", label: "Fraud detection", show: isAdmin },
    ],
  },
];

export default function AppShell({ children }: { children: React.ReactNode }) {
  const { token, me, ready } = useMe();
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    if (ready && !token) router.replace("/login");
  }, [ready, token, router]);

  if (!ready) {
    return <Centered>Loading…</Centered>;
  }
  if (!me) {
    return <Centered>Redirecting…</Centered>;
  }

  return (
    <div style={{ display: "flex", minHeight: "100vh" }}>
      <aside
        style={{
          width: 232,
          flexShrink: 0,
          borderRight: "1px solid var(--border)",
          background: "var(--surface)",
          padding: "1.1rem 0.9rem",
          display: "flex",
          flexDirection: "column",
          gap: "0.4rem",
        }}
      >
        <a href="/dashboard" style={{ color: "var(--text)", fontWeight: 700, fontSize: "1.05rem", padding: "0 0.5rem 0.6rem" }}>
          TradeBridge
        </a>
        {NAV.map((section) => {
          const items = section.items.filter((it) => it.show(me));
          if (items.length === 0) return null;
          return (
            <div key={section.group} style={{ marginTop: section.group ? "0.7rem" : 0 }}>
              {section.group && (
                <div className="faint" style={{ fontSize: "0.68rem", textTransform: "uppercase", letterSpacing: "0.06em", padding: "0 0.5rem 0.3rem" }}>
                  {section.group}
                </div>
              )}
              {items.map((it) => {
                const active = pathname === it.href || pathname.startsWith(it.href + "/");
                return (
                  <a
                    key={it.href}
                    href={it.href}
                    style={{
                      display: "block",
                      padding: "0.45rem 0.6rem",
                      borderRadius: 7,
                      fontSize: "0.9rem",
                      fontWeight: 550,
                      color: active ? "var(--text)" : "var(--muted)",
                      background: active ? "var(--surface-2)" : "transparent",
                    }}
                  >
                    {it.label}
                  </a>
                );
              })}
            </div>
          );
        })}
      </aside>

      <div style={{ flex: 1, display: "flex", flexDirection: "column", minWidth: 0 }}>
        <header
          style={{
            height: 56,
            borderBottom: "1px solid var(--border)",
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            padding: "0 1.5rem",
            gap: "1rem",
          }}
        >
          <div className="row" style={{ gap: "0.6rem", minWidth: 0 }}>
            <strong style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
              {me.company_name ?? "—"}
            </strong>
            <span className="badge badge-info">{me.company_type ?? "company"}</span>
            <span className="badge">{me.role}</span>
          </div>
          <button
            className="btn btn-sm btn-ghost"
            onClick={() => {
              logout();
              router.replace("/login");
            }}
          >
            Sign out
          </button>
        </header>
        <main style={{ padding: "1.75rem", maxWidth: 1000, width: "100%" }}>{children}</main>
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

export function PageHeader({ title, subtitle, action }: { title: string; subtitle?: string; action?: React.ReactNode }) {
  return (
    <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start", marginBottom: "1.25rem" }}>
      <div>
        <h1 style={{ fontSize: "1.4rem" }}>{title}</h1>
        {subtitle && <p className="muted" style={{ margin: "0.25rem 0 0" }}>{subtitle}</p>}
      </div>
      {action}
    </div>
  );
}
