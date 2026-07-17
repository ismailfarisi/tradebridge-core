"use client";

import { useEffect, useState } from "react";
import AdminShell from "@/components/AdminShell";
import { listAudit, useAdminToken, type AuditEntry } from "@/lib/admin";

export default function AdminAuditPage() {
  return (
    <AdminShell>
      <AuditConsole />
    </AdminShell>
  );
}

function AuditConsole() {
  const token = useAdminToken();
  const [companyId, setCompanyId] = useState("");
  const [actor, setActor] = useState("");
  const [action, setAction] = useState("");
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!token) return;
    listAudit(token, { company_id: companyId, actor, action })
      .then((r) => setEntries(r.entries))
      .catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  }, [token, companyId, actor, action]);

  return (
    <>
      <div style={{ marginBottom: "1.25rem" }}>
        <h1 style={{ fontSize: "1.4rem" }}>Audit log</h1>
        <p className="muted" style={{ margin: "0.25rem 0 0" }}>Every platform-admin action, append-only.</p>
      </div>

      <div className="row" style={{ gap: "0.75rem", marginBottom: "1rem", flexWrap: "wrap" }}>
        <div className="field">
          <label>Company ID</label>
          <input className="input" value={companyId} onChange={(e) => setCompanyId(e.target.value)} placeholder="co_…" />
        </div>
        <div className="field">
          <label>Actor</label>
          <input className="input" value={actor} onChange={(e) => setActor(e.target.value)} placeholder="admin:ops@…" />
        </div>
        <div className="field">
          <label>Action</label>
          <input className="input" value={action} onChange={(e) => setAction(e.target.value)} placeholder="company.suspend" />
        </div>
      </div>

      {err && <p className="err">{err}</p>}

      <div className="card">
        <table className="tbl">
          <thead>
            <tr>
              <th>Time</th>
              <th>Actor</th>
              <th>Action</th>
              <th>Target</th>
              <th>Detail</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((e) => (
              <tr key={e.id}>
                <td className="muted small">{new Date(e.created_at).toLocaleString()}</td>
                <td className="small">{e.actor}</td>
                <td><span className="badge badge-info">{e.action}</span></td>
                <td className="muted small">{e.target_id ?? "—"}</td>
                <td className="muted small" style={{ maxWidth: 320, overflowWrap: "anywhere" }}>{formatDetail(e.detail)}</td>
              </tr>
            ))}
            {entries.length === 0 && (
              <tr><td colSpan={5} className="muted">No matching audit entries.</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}

// formatDetail renders the entry's `detail` payload as readable JSON. The
// backend stores it as a jsonb column but plain encoding/json serializes a Go
// []byte field as base64, so the wire value may be either a parsed object or
// a base64 string depending on backend version — handle both defensively.
function formatDetail(detail: unknown): string {
  if (detail == null) return "—";
  if (typeof detail === "string") {
    try {
      const decoded = atob(detail);
      return JSON.stringify(JSON.parse(decoded));
    } catch {
      return detail;
    }
  }
  try {
    return JSON.stringify(detail);
  } catch {
    return String(detail);
  }
}
