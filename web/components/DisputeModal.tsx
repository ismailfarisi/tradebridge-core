"use client";

import { useState } from "react";

const REASONS = ["not_delivered", "not_as_described", "payment", "other"] as const;

// DisputeModal is a simple overlay for raising a dispute on an order: a
// reason select + detail textarea. The caller owns persistence via onSubmit.
export function DisputeModal({
  onSubmit,
  onClose,
}: {
  onSubmit: (reason: string, detail: string) => Promise<void>;
  onClose: () => void;
}) {
  const [reason, setReason] = useState<(typeof REASONS)[number]>("not_delivered");
  const [detail, setDetail] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      await onSubmit(reason, detail);
      onClose();
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "dispute failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      style={{
        position: "fixed", inset: 0, background: "rgba(0,0,0,0.6)",
        display: "flex", alignItems: "center", justifyContent: "center", padding: "1rem", zIndex: 50,
      }}
    >
      <form className="card" onClick={(e) => e.stopPropagation()} onSubmit={submit} style={{ maxWidth: 420, width: "100%" }}>
        <h2 style={{ fontSize: "1.2rem", marginBottom: "0.75rem" }}>Open a dispute</h2>
        <div className="field">
          <label>Reason</label>
          <select className="input" value={reason} onChange={(e) => setReason(e.target.value as (typeof REASONS)[number])}>
            {REASONS.map((r) => <option key={r} value={r}>{r.replace(/_/g, " ")}</option>)}
          </select>
        </div>
        <div className="field">
          <label>Detail</label>
          <textarea className="input" rows={4} value={detail} onChange={(e) => setDetail(e.target.value)} placeholder="What happened?" />
        </div>
        {err && <p className="err">{err}</p>}
        <div className="row" style={{ gap: "0.6rem", justifyContent: "flex-end", marginTop: "0.5rem" }}>
          <button type="button" className="btn btn-ghost" onClick={onClose} disabled={busy}>Cancel</button>
          <button className="btn btn-primary" disabled={busy}>{busy ? "Submitting…" : "Submit dispute"}</button>
        </div>
      </form>
    </div>
  );
}
