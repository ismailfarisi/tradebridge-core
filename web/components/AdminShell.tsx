"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { adminMe, clearAdminToken, useAdminToken } from "@/lib/admin";

const NAV = [
  { href: "/admin/companies", label: "Companies" },
  { href: "/admin/verifications", label: "Verifications" },
  { href: "/admin/disputes", label: "Disputes" },
  { href: "/admin/reports", label: "Reports" },
  { href: "/admin/audit", label: "Audit" },
  { href: "/admin/analytics", label: "Analytics" },
  { href: "/admin/waitlist", label: "Waitlist" },
  { href: "/admin/feedback", label: "Feedback" },
  { href: "/admin", label: "Settings" },
];

export default function AdminShell({ children }: { children: React.ReactNode }) {
  const token = useAdminToken();
  const router = useRouter();
  const pathname = usePathname();
  const [hydrated, setHydrated] = useState(false);
  const [ok, setOk] = useState(false);

  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => setHydrated(true), []);

  useEffect(() => {
    if (!hydrated) return;
    if (!token) { router.replace("/admin/login"); return; }
    adminMe(token).then(() => setOk(true)).catch(() => { clearAdminToken(); router.replace("/admin/login"); });
  }, [hydrated, token, router]);

  if (!hydrated || !ok) {
    return <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center" }} className="muted">Loading…</div>;
  }

  return (
    <div style={{ minHeight: "100vh" }}>
      <header style={{ height: 56, borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", justifyContent: "space-between", padding: "0 1.5rem" }}>
        <div className="row" style={{ gap: "1.25rem", alignItems: "center" }}>
          <strong>TradeBridge · Platform Admin</strong>
          <nav className="row" style={{ gap: "0.75rem" }}>
            {NAV.map((it) => {
              const active = pathname === it.href;
              return (
                <Link
                  key={it.href}
                  href={it.href}
                  className="small"
                  style={{ color: active ? "var(--text)" : "var(--muted)", fontWeight: active ? 600 : 500 }}
                >
                  {it.label}
                </Link>
              );
            })}
          </nav>
        </div>
        <button className="btn btn-sm btn-ghost" onClick={() => { clearAdminToken(); router.replace("/admin/login"); }}>Sign out</button>
      </header>
      <main style={{ padding: "1.75rem", maxWidth: 1100, width: "100%" }}>{children}</main>
    </div>
  );
}
