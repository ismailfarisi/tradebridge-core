"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { logout, useMe } from "@/lib/auth";
import { listRfqs, type Me, type RFQ } from "@/lib/api";
import { useCoreHealth } from "@/lib/health";
import NotificationBell from "@/components/NotificationBell";
import VerifyEmailBanner from "@/components/VerifyEmailBanner";

const isSeller = (m: Me) => m.company_type === "seller" || m.company_type === "both";

export default function ChatShell({ children }: { children: React.ReactNode }) {
  const { token, me, ready } = useMe();
  const router = useRouter();
  const pathname = usePathname();
  const [hydrated, setHydrated] = useState(false);
  const [rfqs, setRfqs] = useState<RFQ[]>([]);
  const online = useCoreHealth();

  // Client-mount guard (see AppShell): set once after hydration so a logged-in
  // user isn't bounced to /login on the first server-snapshot render.
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => setHydrated(true), []);
  useEffect(() => {
    if (hydrated && ready && !token) router.replace("/login");
  }, [hydrated, ready, token, router]);

  // Refresh the thread list on navigation (a new thread appears after create).
  useEffect(() => {
    if (!token) return;
    listRfqs(token).then((r) => setRfqs(r.rfqs)).catch(() => {});
  }, [token, pathname]);

  if (!hydrated || !ready) return <Centered>Loading…</Centered>;
  if (!me) return <Centered>Redirecting…</Centered>;

  const activeId = pathname.startsWith("/chat/") ? pathname.slice("/chat/".length) : null;
  const title =
    activeId ? rfqs.find((r) => r.id === activeId)?.title ?? "RFQ" : "New RFQ";

  return (
    <div style={{ display: "flex", height: "100vh" }}>
      <aside
        style={{
          width: 264,
          flexShrink: 0,
          borderRight: "1px solid var(--border)",
          background: "var(--surface)",
          display: "flex",
          flexDirection: "column",
        }}
      >
        <div style={{ padding: "0.9rem" }}>
          <Link
            href="/chat"
            className="btn btn-primary"
            style={{ display: "block", textAlign: "center", width: "100%" }}
          >
            ＋ New RFQ
          </Link>
        </div>
        <nav style={{ flex: 1, overflowY: "auto", padding: "0 0.6rem", display: "flex", flexDirection: "column", gap: "0.2rem" }}>
          {rfqs.map((r) => {
            const active = r.id === activeId;
            return (
              <Link
                key={r.id}
                href={`/chat/${r.id}`}
                style={{
                  display: "block",
                  padding: "0.5rem 0.6rem",
                  borderRadius: 7,
                  fontSize: "0.88rem",
                  color: active ? "var(--text)" : "var(--muted)",
                  background: active ? "var(--surface-2)" : "transparent",
                  whiteSpace: "nowrap",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                }}
              >
                {r.title || "Untitled RFQ"}
              </Link>
            );
          })}
          {rfqs.length === 0 && (
            <p className="muted small" style={{ padding: "0.5rem 0.6rem" }}>
              No RFQs yet — start one above.
            </p>
          )}
        </nav>
        <div style={{ borderTop: "1px solid var(--border)", padding: "0.7rem 0.9rem", display: "flex", flexDirection: "column", gap: "0.35rem" }}>
          <strong style={{ fontSize: "0.9rem", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
            {me.company_name ?? "—"}
          </strong>
          {isSeller(me) && <Link href="/seller/profile" className="small muted">Seller console</Link>}
          <button
            className="btn btn-sm btn-ghost"
            style={{ marginTop: "0.25rem", alignSelf: "flex-start" }}
            onClick={() => {
              logout();
              router.replace("/login");
            }}
          >
            Sign out
          </button>
        </div>
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
          <strong style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{title}</strong>
          <div className="row" style={{ gap: "0.6rem", alignItems: "center" }}>
            <StatusDot online={online} />
            <span className="badge badge-info">{me.company_type ?? "buyer"}</span>
            {token && <NotificationBell token={token} />}
          </div>
        </header>
        {token && <VerifyEmailBanner token={token} emailVerified={me.email_verified} />}
        <main style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", paddingBottom: "var(--cookie-banner, 0px)" }}>{children}</main>
      </div>
    </div>
  );
}

function StatusDot({ online }: { online: boolean | null }) {
  const color = online == null ? "var(--muted)" : online ? "#22c55e" : "#ef4444";
  const label = online == null ? "Checking…" : online ? "Connected" : "Core offline";
  return (
    <span className="row small muted" style={{ gap: "0.4rem", alignItems: "center" }}>
      <span style={{ width: 8, height: 8, borderRadius: "50%", background: color, display: "inline-block" }} />
      {label}
    </span>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center" }} className="muted">
      {children}
    </div>
  );
}
