"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import AdminShell from "@/components/AdminShell";
import { startImpersonation } from "@/lib/impersonation";
import {
  getCompany,
  impersonate,
  suspendCompany,
  unsuspendCompany,
  useAdminToken,
  type AdminCompanyDetail,
} from "@/lib/admin";

export default function AdminCompanyDetailPage() {
  return (
    <AdminShell>
      <CompanyDetail />
    </AdminShell>
  );
}

function CompanyDetail() {
  const token = useAdminToken();
  const params = useParams<{ id: string }>();
  const id = params.id;

  const [company, setCompany] = useState<AdminCompanyDetail | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [popupBlocked, setPopupBlocked] = useState(false);

  const load = () => {
    if (!token || !id) return;
    getCompany(token, id).then(setCompany).catch(() => setNotFound(true));
  };

  useEffect(() => {
    if (!token || !id) return;
    getCompany(token, id).then(setCompany).catch(() => setNotFound(true));
  }, [token, id]);

  const suspend = async () => {
    if (!token) return;
    const reason = window.prompt("Reason for suspending this company?");
    if (reason === null) return;
    setBusy("suspend");
    setErr(null);
    try {
      await suspendCompany(token, id, reason);
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "suspend failed");
    } finally {
      setBusy(null);
    }
  };

  const unsuspend = async () => {
    if (!token) return;
    setBusy("unsuspend");
    setErr(null);
    try {
      await unsuspendCompany(token, id);
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "unsuspend failed");
    } finally {
      setBusy(null);
    }
  };

  const doImpersonate = async () => {
    if (!token || !company) return;
    // Open the tab synchronously, in direct response to the click — a
    // pop-up opened only after the `await impersonate(...)` below is no
    // longer treated as user-gesture-triggered by Safari/Firefox and gets
    // silently blocked (the token would then install with nothing opening).
    const win = window.open("", "_blank");
    setBusy("impersonate");
    setErr(null);
    setPopupBlocked(false);
    try {
      const res = await impersonate(token, id);
      const opened = startImpersonation(win, res.token, company.name);
      if (!opened) setPopupBlocked(true);
    } catch (e) {
      win?.close();
      setErr(e instanceof Error ? e.message : "impersonate failed");
    } finally {
      setBusy(null);
    }
  };

  if (notFound) return <p className="err">Company not found.</p>;
  if (!company) return <p className="muted">Loading…</p>;

  return (
    <>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start", marginBottom: "1.25rem" }}>
        <div>
          <h1 style={{ fontSize: "1.4rem" }}>{company.name}</h1>
          <p className="muted" style={{ margin: "0.25rem 0 0" }}>{company.id}</p>
        </div>
        <div className="row" style={{ gap: "0.5rem" }}>
          <button className="btn btn-sm" disabled={!!busy} onClick={doImpersonate}>
            {busy === "impersonate" ? "…" : "Impersonate"}
          </button>
          {company.status === "suspended" ? (
            <button className="btn btn-sm btn-primary" disabled={!!busy} onClick={unsuspend}>
              {busy === "unsuspend" ? "…" : "Unsuspend"}
            </button>
          ) : (
            <button className="btn btn-sm btn-ghost" disabled={!!busy} onClick={suspend}>
              {busy === "suspend" ? "…" : "Suspend"}
            </button>
          )}
        </div>
      </div>

      {err && <p className="err">{err}</p>}
      {popupBlocked && (
        <p className="err">
          Pop-up blocked — the read-only session was installed anyway.{" "}
          <a href="/dashboard" target="_blank" rel="noreferrer">Open /dashboard manually</a> to view as this company.
        </p>
      )}

      <div className="card">
        <div className="row" style={{ gap: "1.5rem", flexWrap: "wrap" }}>
          <Stat label="Type" value={company.type} />
          <Stat label="Status" value={<span className={`badge ${company.status === "suspended" ? "badge-warning" : "badge-success"}`}>{company.status}</span>} />
          <Stat label="Plan" value={company.plan_code || "—"} />
          <Stat label="Verification" value={<span className={`badge ${company.verification_status === "verified" ? "badge-success" : company.verification_status === "rejected" ? "badge-danger" : "badge-info"}`}>{company.verification_status}</span>} />
        </div>
      </div>

      <div className="card" style={{ marginTop: "1rem" }}>
        <h2>Activity</h2>
        <div className="row" style={{ gap: "1.5rem", flexWrap: "wrap" }}>
          <Stat label="RFQs" value={company.counts.rfqs} />
          <Stat label="Quotes" value={company.counts.quotes} />
          <Stat label="Orders" value={company.counts.orders} />
        </div>
      </div>

      <div className="card" style={{ marginTop: "1rem" }}>
        <h2>Users</h2>
        <table className="tbl">
          <thead>
            <tr><th>Email</th><th>Role</th></tr>
          </thead>
          <tbody>
            {company.users.map((u) => (
              <tr key={u.id}><td>{u.email}</td><td className="muted small">{u.role}</td></tr>
            ))}
            {company.users.length === 0 && (
              <tr><td colSpan={2} className="muted">No users.</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="stat">
      <div className="muted small">{label}</div>
      <div style={{ fontWeight: 600 }}>{value}</div>
    </div>
  );
}
