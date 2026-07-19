"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import AdminShell from "@/components/AdminShell";
import { listReports, resolveReport, useAdminToken, type AdminReport } from "@/lib/admin";

const STATUSES = ["open", "reviewed", "dismissed", "actioned", ""] as const;
const RESOLUTIONS = ["reviewed", "dismissed", "actioned"] as const;

export default function AdminReportsPage() {
  return (
    <AdminShell>
      <ReportsConsole />
    </AdminShell>
  );
}

function ReportsConsole() {
  const token = useAdminToken();
  const [status, setStatus] = useState<(typeof STATUSES)[number]>("open");
  const [reports, setReports] = useState<AdminReport[]>([]);
  const [err, setErr] = useState<string | null>(null);

  const load = () => {
    if (!token) return;
    listReports(token, status)
      .then((r) => setReports(r.reports ?? []))
      .catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  };

  useEffect(() => {
    if (!token) return;
    listReports(token, status)
      .then((r) => setReports(r.reports ?? []))
      .catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  }, [token, status]);

  return (
    <>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start", marginBottom: "1.25rem" }}>
        <div>
          <h1 style={{ fontSize: "1.4rem" }}>Reports</h1>
          <p className="muted" style={{ margin: "0.25rem 0 0" }}>Company-to-company abuse reports.</p>
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
              <th>Reported</th>
              <th>Reporter</th>
              <th>Reason</th>
              <th>Detail</th>
              <th>Status</th>
              <th>Raised</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {reports.map((r) => (
              <ReportRow key={r.id} report={r} token={token} onResolved={load} />
            ))}
            {reports.length === 0 && (
              <tr><td colSpan={7} className="muted">No reports in this queue.</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}

function ReportRow({ report, token, onResolved }: { report: AdminReport; token: string | null; onResolved: () => void }) {
  const [busy, setBusy] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const resolve = async (newStatus: (typeof RESOLUTIONS)[number]) => {
    if (!token) return;
    setBusy(newStatus);
    setErr(null);
    try {
      await resolveReport(token, report.id, newStatus);
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
        <td><Link href={`/admin/companies/${report.reported_company_id}`}>{report.reported_name}</Link></td>
        <td><Link href={`/admin/companies/${report.reporter_company_id}`}>{report.reporter_name}</Link></td>
        <td className="muted small">{report.reason}</td>
        <td className="muted small">{report.detail ?? "—"}</td>
        <td><span className={`badge ${report.status === "open" ? "badge-warning" : "badge-success"}`}>{report.status}</span></td>
        <td className="muted small">{new Date(report.created_at).toLocaleString()}</td>
        <td style={{ textAlign: "right" }}>
          {report.status === "open" ? (
            <div className="row" style={{ gap: "0.4rem", justifyContent: "flex-end" }}>
              <button className="btn btn-sm btn-primary" disabled={!!busy} onClick={() => resolve("actioned")}>
                {busy === "actioned" ? "…" : "Action"}
              </button>
              <button className="btn btn-sm" disabled={!!busy} onClick={() => resolve("reviewed")}>
                {busy === "reviewed" ? "…" : "Reviewed"}
              </button>
              <button className="btn btn-sm btn-ghost" disabled={!!busy} onClick={() => resolve("dismissed")}>
                {busy === "dismissed" ? "…" : "Dismiss"}
              </button>
            </div>
          ) : (
            <span className="muted small">{report.resolved_at ? new Date(report.resolved_at).toLocaleString() : ""}</span>
          )}
        </td>
      </tr>
      {err && (
        <tr><td colSpan={7} className="err small">{err}</td></tr>
      )}
    </>
  );
}
