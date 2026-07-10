"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { adminMe, clearAdminToken, useAdminToken } from "@/lib/admin";

export default function AdminShell({ children }: { children: React.ReactNode }) {
  const token = useAdminToken();
  const router = useRouter();
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
        <strong>TradeBridge · Platform Admin</strong>
        <button className="btn btn-sm btn-ghost" onClick={() => { clearAdminToken(); router.replace("/admin/login"); }}>Sign out</button>
      </header>
      <main style={{ padding: "1.75rem", maxWidth: 760, width: "100%" }}>{children}</main>
    </div>
  );
}
