"use client";

import { useState } from "react";
import { useToken } from "@/lib/auth";
import { createBlock, createReport } from "@/lib/trust";

const REASONS = ["spam", "fraud", "abuse", "inappropriate", "other"] as const;

// ReportBlockActions shows Block + Report controls for a seller. Renders nothing
// when no viewer is signed in (both actions require auth).
export function ReportBlockActions({ companyId }: { companyId: string }) {
  const token = useToken();
  const [showReport, setShowReport] = useState(false);
  const [reason, setReason] = useState<(typeof REASONS)[number]>("spam");
  const [detail, setDetail] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  if (!token) return null;

  const block = async () => {
    setBusy(true); setErr(null); setMsg(null);
    try {
      await createBlock(token, companyId);
      setMsg("Blocked. You won't be matched with this company.");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "block failed");
    } finally { setBusy(false); }
  };

  const submitReport = async () => {
    setBusy(true); setErr(null); setMsg(null);
    try {
      await createReport(token, companyId, reason, detail);
      setMsg("Report submitted. Our team will review it.");
      setShowReport(false);
      setDetail("");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "report failed");
    } finally { setBusy(false); }
  };

  return (
    <section className="card" style={{ marginTop: "1rem" }}>
      <div className="row" style={{ gap: "0.5rem" }}>
        <button className="btn" onClick={block} disabled={busy}>Block</button>
        <button className="btn" onClick={() => setShowReport((s) => !s)} disabled={busy}>Report</button>
      </div>
      {showReport && (
        <div className="col" style={{ marginTop: "0.7rem" }}>
          <div className="field">
            <label>Reason</label>
            <select className="input" value={reason} onChange={(e) => setReason(e.target.value as (typeof REASONS)[number])}>
              {REASONS.map((r) => <option key={r} value={r}>{r}</option>)}
            </select>
          </div>
          <div className="field">
            <label>Detail (optional)</label>
            <textarea className="input" rows={3} value={detail} onChange={(e) => setDetail(e.target.value)} />
          </div>
          <div><button className="btn btn-primary" onClick={submitReport} disabled={busy}>{busy ? "Submitting…" : "Submit report"}</button></div>
        </div>
      )}
      {msg && <p className="ok">{msg}</p>}
      {err && <p className="err">{err}</p>}
    </section>
  );
}
