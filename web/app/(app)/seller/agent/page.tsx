"use client";

import { useEffect, useState } from "react";
import { PageHeader } from "@/components/AppShell";
import { useToken } from "@/lib/auth";
import { issueAgentCred, listAgentCreds, revokeAgentCred, type AgentCred } from "@/lib/api";

export default function AgentPage() {
  const token = useToken();
  const [creds, setCreds] = useState<AgentCred[]>([]);
  const [label, setLabel] = useState("");
  const [issued, setIssued] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const refresh = () => {
    if (!token) return;
    listAgentCreds(token).then((r) => setCreds(r.credentials)).catch((e) => setErr(e.message));
  };

  useEffect(() => {
    if (!token) return;
    listAgentCreds(token).then((r) => setCreds(r.credentials)).catch((e) => setErr(e.message));
  }, [token]);

  const issue = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true);
    setErr(null);
    setIssued(null);
    try {
      const r = await issueAgentCred(token, label || "default");
      setIssued(r.secret);
      setLabel("");
      refresh();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "failed");
    } finally {
      setBusy(false);
    }
  };

  const revoke = async (id: string) => {
    if (!token) return;
    await revokeAgentCred(token, id).catch(() => {});
    refresh();
  };

  return (
    <>
      <PageHeader title="Agent credentials" subtitle="Your seller agent connects to the Gateway with one of these secrets." />

      <div className="card" style={{ marginBottom: "1.5rem" }}>
        <h2>Issue a credential</h2>
        <form className="row" onSubmit={issue} style={{ marginTop: "0.75rem", alignItems: "flex-end" }}>
          <div className="field grow"><label>Label</label><input className="input" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="production" /></div>
          <button className="btn btn-primary" disabled={busy}>{busy ? "Issuing…" : "Issue"}</button>
        </form>
        {issued && (
          <div className="notice" style={{ marginTop: "0.85rem", borderColor: "var(--primary)" }}>
            <strong>Copy this now — it&apos;s shown once:</strong>
            <div style={{ marginTop: "0.4rem", wordBreak: "break-all", fontFamily: "monospace", color: "var(--text)" }}>{issued}</div>
            <div className="small" style={{ marginTop: "0.4rem" }}>Set it as <code>AGENT_TOKEN</code> in the agent&apos;s config.</div>
          </div>
        )}
        {err && <p className="err" style={{ marginTop: "0.5rem" }}>{err}</p>}
      </div>

      <div className="card">
        <h2>Credentials</h2>
        <table className="tbl" style={{ marginTop: "0.5rem" }}>
          <thead><tr><th>Label</th><th>ID</th><th>Status</th><th></th></tr></thead>
          <tbody>
            {creds.map((c) => (
              <tr key={c.id}>
                <td>{c.label}</td>
                <td className="muted small" style={{ fontFamily: "monospace" }}>{c.id}</td>
                <td><span className={c.status === "active" ? "badge badge-success" : "badge"}>{c.status}</span></td>
                <td style={{ textAlign: "right" }}>
                  {c.status === "active" && <button className="btn btn-sm btn-danger" onClick={() => revoke(c.id)}>Revoke</button>}
                </td>
              </tr>
            ))}
            {creds.length === 0 && <tr><td colSpan={4} className="muted">No credentials yet.</td></tr>}
          </tbody>
        </table>
      </div>
    </>
  );
}
