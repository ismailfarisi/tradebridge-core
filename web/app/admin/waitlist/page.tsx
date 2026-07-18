"use client";

import { useEffect, useState } from "react";
import AdminShell from "@/components/AdminShell";
import { useAdminToken } from "@/lib/admin";
import { listWaitlist, inviteWaitlist, type WaitlistSignup, type WaitlistStatus } from "@/lib/waitlistAdmin";

const FILTERS: { value: WaitlistStatus; label: string }[] = [
  { value: "pending", label: "Pending" },
  { value: "invited", label: "Invited" },
  { value: "", label: "All" },
];

export default function AdminWaitlistPage() {
  return (
    <AdminShell>
      <WaitlistConsole />
    </AdminShell>
  );
}

function WaitlistConsole() {
  const token = useAdminToken();
  const [status, setStatus] = useState<WaitlistStatus>("pending");
  const [signups, setSignups] = useState<WaitlistSignup[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState<"selected" | "all" | null>(null);
  const [lastResult, setLastResult] = useState<{ sent: number; attempted: number } | null>(null);

  const load = () => {
    if (!token) return;
    listWaitlist(token, status)
      .then((r) => {
        setSignups(r.signups);
        setSelected(new Set());
      })
      .catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  };

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { load(); }, [token, status]);

  const toggle = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const invite = async (ids?: string[]) => {
    if (!token) return;
    setBusy(ids ? "selected" : "all");
    setErr(null);
    try {
      const result = await inviteWaitlist(token, ids);
      setLastResult(result);
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "invite failed");
    } finally {
      setBusy(null);
    }
  };

  const pendingCount = signups.filter((s) => !s.invited_at).length;

  return (
    <>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start", marginBottom: "1.25rem" }}>
        <div>
          <h1 style={{ fontSize: "1.4rem" }}>Waitlist</h1>
          <p className="muted" style={{ margin: "0.25rem 0 0" }}>Launch invite: email pre-signups when the marketplace opens.</p>
        </div>
        <div className="field" style={{ minWidth: 140 }}>
          <select className="input" value={status} onChange={(e) => setStatus(e.target.value as WaitlistStatus)}>
            {FILTERS.map((f) => <option key={f.value} value={f.value}>{f.label}</option>)}
          </select>
        </div>
      </div>

      <div className="row" style={{ gap: "0.5rem", marginBottom: "1rem" }}>
        <button
          className="btn btn-primary btn-sm"
          disabled={selected.size === 0 || !!busy}
          onClick={() => invite(Array.from(selected))}
        >
          {busy === "selected" ? "…" : `Invite selected (${selected.size})`}
        </button>
        <button
          className="btn btn-sm"
          disabled={pendingCount === 0 || !!busy}
          onClick={() => invite(undefined)}
        >
          {busy === "all" ? "…" : "Invite all pending"}
        </button>
        {lastResult && (
          <span className="muted small">Last run: sent {lastResult.sent} of {lastResult.attempted}.</span>
        )}
      </div>

      {err && <p className="err">{err}</p>}

      <div className="card">
        <table className="tbl">
          <thead>
            <tr>
              <th></th>
              <th>Email</th>
              <th>Source</th>
              <th>Signed up</th>
              <th>Invited</th>
            </tr>
          </thead>
          <tbody>
            {signups.map((s) => (
              <tr key={s.id}>
                <td>
                  {!s.invited_at && (
                    <input type="checkbox" checked={selected.has(s.id)} onChange={() => toggle(s.id)} />
                  )}
                </td>
                <td>{s.email}</td>
                <td className="muted small">{s.source ?? "—"}</td>
                <td className="muted small">{new Date(s.created_at).toLocaleString()}</td>
                <td>
                  {s.invited_at ? (
                    <span className="badge badge-success">{new Date(s.invited_at).toLocaleString()}</span>
                  ) : (
                    <span className="badge badge-warning">pending</span>
                  )}
                </td>
              </tr>
            ))}
            {signups.length === 0 && (
              <tr><td colSpan={5} className="muted">No signups in this view.</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}
