"use client";

import { useEffect, useState } from "react";
import AdminShell from "@/components/AdminShell";
import { listDisputes, resolveDispute, useAdminToken } from "@/lib/admin";
import type { Dispute } from "@/lib/orders";

export default function AdminDisputesPage() {
  return (
    <AdminShell>
      <DisputesConsole />
    </AdminShell>
  );
}

const FILTERS = ["open", "all"] as const;

function DisputesConsole() {
  const token = useAdminToken();
  const [filter, setFilter] = useState<(typeof FILTERS)[number]>("open");
  const [disputes, setDisputes] = useState<Dispute[]>([]);
  const [err, setErr] = useState<string | null>(null);

  const load = () => {
    if (!token) return;
    listDisputes(token, filter === "all" ? "" : filter)
      .then((r) => setDisputes(r.disputes))
      .catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  };

  useEffect(() => {
    if (!token) return;
    listDisputes(token, filter === "all" ? "" : filter)
      .then((r) => setDisputes(r.disputes))
      .catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  }, [token, filter]);

  return (
    <>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start", marginBottom: "1.25rem" }}>
        <div>
          <h1 style={{ fontSize: "1.4rem" }}>Disputes</h1>
          <p className="muted" style={{ margin: "0.25rem 0 0" }}>Party-raised disputes on orders.</p>
        </div>
        <div className="field" style={{ minWidth: 140 }}>
          <select className="input" value={filter} onChange={(e) => setFilter(e.target.value as (typeof FILTERS)[number])}>
            {FILTERS.map((f) => <option key={f} value={f}>{f}</option>)}
          </select>
        </div>
      </div>

      {err && <p className="err">{err}</p>}

      <div className="card">
        <table className="tbl">
          <thead>
            <tr>
              <th>Dispute</th>
              <th>Order</th>
              <th>Reason</th>
              <th>Status</th>
              <th>Raised</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {disputes.map((d) => (
              <DisputeRow key={d.id} dispute={d} token={token} onResolved={load} />
            ))}
            {disputes.length === 0 && (
              <tr><td colSpan={6} className="muted">No disputes.</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}

const OUTCOMES = ["resolved", "rejected"] as const;
const ORDER_TOS = ["completed", "cancelled"] as const;

function DisputeRow({ dispute, token, onResolved }: { dispute: Dispute; token: string | null; onResolved: () => void }) {
  const [showResolve, setShowResolve] = useState(false);
  const [outcome, setOutcome] = useState<(typeof OUTCOMES)[number]>("resolved");
  const [orderTo, setOrderTo] = useState<(typeof ORDER_TOS)[number]>("completed");
  const [resolution, setResolution] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const submit = async () => {
    if (!token) return;
    setBusy(true);
    setErr(null);
    try {
      await resolveDispute(token, dispute.id, outcome, orderTo, resolution);
      setShowResolve(false);
      onResolved();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "resolve failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <tr>
        <td>{dispute.id.slice(0, 8)}</td>
        <td>{dispute.order_id.slice(0, 8)}</td>
        <td className="muted small">{dispute.reason.replace(/_/g, " ")}</td>
        <td>
          <span className={`badge ${dispute.status === "open" ? "badge-warning" : "badge-success"}`}>{dispute.status}</span>
        </td>
        <td className="muted small">{new Date(dispute.created_at).toLocaleString()}</td>
        <td style={{ textAlign: "right" }}>
          {dispute.status === "open" ? (
            <button className="btn btn-sm btn-primary" onClick={() => setShowResolve((s) => !s)}>Resolve</button>
          ) : (
            <span className="muted small">{dispute.resolved_by_admin_id ? `by ${dispute.resolved_by_admin_id.slice(0, 8)}` : ""}</span>
          )}
        </td>
      </tr>
      {dispute.status !== "open" && dispute.resolution && (
        <tr>
          <td colSpan={6} className="muted small">Resolution: {dispute.resolution}</td>
        </tr>
      )}
      {showResolve && (
        <tr>
          <td colSpan={6}>
            <div className="col" style={{ gap: "0.5rem", padding: "0.5rem 0" }}>
              <p className="small">{dispute.detail}</p>
              <div className="row" style={{ gap: "0.75rem", flexWrap: "wrap" }}>
                <div className="field">
                  <label>Outcome</label>
                  <select className="input" value={outcome} onChange={(e) => setOutcome(e.target.value as (typeof OUTCOMES)[number])}>
                    {OUTCOMES.map((o) => <option key={o} value={o}>{o}</option>)}
                  </select>
                </div>
                <div className="field">
                  <label>Move order to</label>
                  <select className="input" value={orderTo} onChange={(e) => setOrderTo(e.target.value as (typeof ORDER_TOS)[number])}>
                    {ORDER_TOS.map((o) => <option key={o} value={o}>{o}</option>)}
                  </select>
                </div>
              </div>
              <div className="field">
                <label>Resolution note</label>
                <textarea className="input" rows={2} value={resolution} onChange={(e) => setResolution(e.target.value)} />
              </div>
              {err && <p className="err">{err}</p>}
              <div className="row" style={{ gap: "0.5rem" }}>
                <button className="btn btn-primary btn-sm" disabled={busy} onClick={submit}>{busy ? "Resolving…" : "Submit resolution"}</button>
                <button className="btn btn-ghost btn-sm" disabled={busy} onClick={() => setShowResolve(false)}>Cancel</button>
              </div>
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
