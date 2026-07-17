"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import AdminShell from "@/components/AdminShell";
import { listCompanies, useAdminToken, type AdminCompany } from "@/lib/admin";

const STATUSES = ["", "active", "suspended"] as const;

export default function AdminCompaniesPage() {
  return (
    <AdminShell>
      <CompaniesConsole />
    </AdminShell>
  );
}

function CompaniesConsole() {
  const token = useAdminToken();
  const [q, setQ] = useState("");
  const [status, setStatus] = useState<(typeof STATUSES)[number]>("");
  const [companies, setCompanies] = useState<AdminCompany[]>([]);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!token) return;
    listCompanies(token, q, status)
      .then((r) => setCompanies(r.companies))
      .catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  }, [token, q, status]);

  return (
    <>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start", marginBottom: "1.25rem" }}>
        <div>
          <h1 style={{ fontSize: "1.4rem" }}>Companies</h1>
          <p className="muted" style={{ margin: "0.25rem 0 0" }}>Search and manage marketplace companies.</p>
        </div>
        <div className="row" style={{ gap: "0.5rem" }}>
          <div className="field">
            <input className="input" placeholder="Search name or ID…" value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
          <div className="field" style={{ minWidth: 140 }}>
            <select className="input" value={status} onChange={(e) => setStatus(e.target.value as (typeof STATUSES)[number])}>
              <option value="">All statuses</option>
              <option value="active">Active</option>
              <option value="suspended">Suspended</option>
            </select>
          </div>
        </div>
      </div>

      {err && <p className="err">{err}</p>}

      <div className="card">
        <table className="tbl">
          <thead>
            <tr>
              <th>Name</th>
              <th>Type</th>
              <th>Status</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {companies.map((c) => (
              <tr key={c.id}>
                <td><Link href={`/admin/companies/${c.id}`}>{c.name}</Link></td>
                <td className="muted small">{c.type}</td>
                <td><span className={`badge ${c.status === "suspended" ? "badge-warning" : "badge-success"}`}>{c.status}</span></td>
                <td className="muted small">{new Date(c.created_at).toLocaleString()}</td>
              </tr>
            ))}
            {companies.length === 0 && (
              <tr><td colSpan={4} className="muted">No companies found.</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}
