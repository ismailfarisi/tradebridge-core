"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import AdminShell from "@/components/AdminShell";
import { listVerifications, resolveVerification, useAdminToken, type VerificationCompany } from "@/lib/admin";

const STATUSES = ["pending", "verified", "rejected", "unverified", ""] as const;

export default function AdminVerificationsPage() {
  return (
    <AdminShell>
      <VerificationsConsole />
    </AdminShell>
  );
}

function VerificationsConsole() {
  const token = useAdminToken();
  const [status, setStatus] = useState<(typeof STATUSES)[number]>("pending");
  const [companies, setCompanies] = useState<VerificationCompany[]>([]);
  const [err, setErr] = useState<string | null>(null);

  const load = () => {
    if (!token) return;
    listVerifications(token, status)
      .then((r) => setCompanies(r.companies))
      .catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  };

  useEffect(() => {
    if (!token) return;
    listVerifications(token, status)
      .then((r) => setCompanies(r.companies))
      .catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  }, [token, status]);

  return (
    <>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start", marginBottom: "1.25rem" }}>
        <div>
          <h1 style={{ fontSize: "1.4rem" }}>Verifications</h1>
          <p className="muted" style={{ margin: "0.25rem 0 0" }}>Domain-verification queue for company badges.</p>
        </div>
        <div className="field" style={{ minWidth: 140 }}>
          <select className="input" value={status} onChange={(e) => setStatus(e.target.value as (typeof STATUSES)[number])}>
            {STATUSES.map((s) => <option key={s} value={s}>{s || "all"}</option>)}
          </select>
        </div>
      </div>

      {err && <p className="err">{err}</p>}

      <div className="card">
        <table className="tbl">
          <thead>
            <tr>
              <th>Company</th>
              <th>Type</th>
              <th>Domain</th>
              <th>Status</th>
              <th>Verified at</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {companies.map((c) => (
              <VerificationRow key={c.id} company={c} token={token} onResolved={load} />
            ))}
            {companies.length === 0 && (
              <tr><td colSpan={6} className="muted">No companies in this queue.</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}

function VerificationRow({ company, token, onResolved }: { company: VerificationCompany; token: string | null; onResolved: () => void }) {
  const [busy, setBusy] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const resolve = async (status: "verified" | "rejected") => {
    if (!token) return;
    let reason = "";
    if (status === "rejected") {
      const input = window.prompt("Reason for rejecting?");
      if (input === null) return; // cancelled — abort, don't reject
      reason = input;
    }
    setBusy(status);
    setErr(null);
    try {
      await resolveVerification(token, company.id, status, reason);
      onResolved();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "resolve failed");
    } finally {
      setBusy(null);
    }
  };

  return (
    <>
      <tr>
        <td><Link href={`/admin/companies/${company.id}`}>{company.name}</Link></td>
        <td className="muted small">{company.type}</td>
        <td className="muted small">{company.verified_domain ?? "—"}</td>
        <td>
          <span className={`badge ${company.verification_status === "verified" ? "badge-success" : company.verification_status === "rejected" ? "badge-danger" : "badge-info"}`}>
            {company.verification_status}
          </span>
        </td>
        <td className="muted small">{company.verified_at ? new Date(company.verified_at).toLocaleString() : "—"}</td>
        <td style={{ textAlign: "right" }}>
          <div className="row" style={{ gap: "0.4rem", justifyContent: "flex-end" }}>
            <button className="btn btn-sm btn-primary" disabled={!!busy} onClick={() => resolve("verified")}>
              {busy === "verified" ? "…" : "Approve"}
            </button>
            <button className="btn btn-sm btn-ghost" disabled={!!busy} onClick={() => resolve("rejected")}>
              {busy === "rejected" ? "…" : "Reject"}
            </button>
          </div>
        </td>
      </tr>
      {err && (
        <tr><td colSpan={6} className="err small">{err}</td></tr>
      )}
    </>
  );
}
