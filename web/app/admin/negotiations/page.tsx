"use client";

import { useEffect, useState } from "react";
import AdminShell from "@/components/AdminShell";
import { useAdminToken } from "@/lib/admin";
import { getKillSwitch, setKillSwitch, type KillSwitchState } from "@/lib/killSwitchAdmin";

export default function AdminNegotiationsPage() {
  return (
    <AdminShell>
      <KillSwitchConsole />
    </AdminShell>
  );
}

// KillSwitchConsole is the platform-wide "pause all negotiations" control:
// while engaged, no buyer's agent counters, accepts or auto-accepts.
function KillSwitchConsole() {
  const token = useAdminToken();
  const [state, setState] = useState<KillSwitchState | null>(null);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!token) return;
    getKillSwitch(token).then(setState).catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  }, [token]);

  const flip = async (paused: boolean) => {
    if (!token) return;
    if (paused && !reason.trim()) {
      setErr("Give a reason — buyers see it.");
      return;
    }
    if (paused && !window.confirm("Pause every automated negotiation on the platform?")) return;
    setBusy(true);
    setErr(null);
    try {
      setState(await setKillSwitch(token, paused, reason.trim()));
      setReason("");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "update failed");
    } finally {
      setBusy(false);
    }
  };

  if (!state) return err ? <p className="err">{err}</p> : <p className="muted">Loading…</p>;
  const c = state.control;

  return (
    <div style={{ maxWidth: 860 }}>
      <h1 style={{ marginTop: 0 }}>Negotiation kill switch</h1>
      <p className="muted small">
        Stops every buyer&apos;s negotiation agent from sending counters, accepting or auto-accepting deals. Buyers see
        that negotiation is paused by the platform and can still accept quotes by hand. Every change is logged below.
      </p>

      <section className="card" style={{ marginBottom: "1rem" }}>
        <div className="row" style={{ justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: "0.75rem" }}>
          <div>
            <span className={c.paused ? "badge badge-danger" : "badge badge-success"}>
              {c.paused ? "Paused platform-wide" : "Negotiations running"}
            </span>
            {c.paused && c.reason && <div className="small" style={{ marginTop: "0.4rem" }}>Reason: {c.reason}</div>}
            {c.updated_by && (
              <div className="faint small" style={{ marginTop: "0.25rem" }}>
                Last changed by {c.updated_by} · {new Date(c.updated_at).toLocaleString()}
              </div>
            )}
          </div>
          {c.paused ? (
            <button className="btn btn-primary" disabled={busy} onClick={() => flip(false)}>Resume negotiations</button>
          ) : (
            <div className="row" style={{ gap: "0.5rem" }}>
              <input
                className="input"
                placeholder="Reason (shown to buyers)"
                value={reason}
                maxLength={500}
                onChange={(e) => setReason(e.target.value)}
                aria-label="Reason for pausing"
              />
              <button className="btn" style={{ color: "#b42318" }} disabled={busy} onClick={() => flip(true)}>
                Pause all negotiations
              </button>
            </div>
          )}
        </div>
        {err && <p className="err small" style={{ marginBottom: 0 }}>{err}</p>}
      </section>

      <section className="card" style={{ padding: 0 }}>
        <h2 style={{ padding: "1rem 1.25rem 0", margin: 0, fontSize: "1rem" }}>History</h2>
        {state.history.length === 0 ? (
          <p className="muted small" style={{ padding: "0 1.25rem 1rem" }}>No changes yet.</p>
        ) : (
          <div style={{ overflowX: "auto" }}>
            <table className="tbl">
              <thead>
                <tr>
                  <th style={{ paddingLeft: "1.25rem" }}>When</th>
                  <th>Scope</th>
                  <th>Change</th>
                  <th>By</th>
                  <th style={{ paddingRight: "1.25rem" }}>Reason</th>
                </tr>
              </thead>
              <tbody>
                {state.history.map((h) => (
                  <tr key={h.id}>
                    <td style={{ paddingLeft: "1.25rem" }} className="small">{new Date(h.created_at).toLocaleString()}</td>
                    <td className="small">{h.scope === "platform" ? "Platform" : h.company_name || h.company_id}</td>
                    <td className="small">{h.paused ? "Paused" : "Resumed"}</td>
                    <td className="small">{h.actor}</td>
                    <td className="small" style={{ paddingRight: "1.25rem" }}>{h.reason || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
