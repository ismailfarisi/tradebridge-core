"use client";

import { useState } from "react";
import { resendVerification } from "@/lib/notifications";

export default function VerifyEmailBanner({
  token, emailVerified,
}: { token: string; emailVerified: boolean | undefined }) {
  const [dismissed, setDismissed] = useState(false);
  const [sent, setSent] = useState(false);
  const [busy, setBusy] = useState(false);

  if (emailVerified !== false || dismissed) return null;

  async function resend() {
    setBusy(true);
    try { await resendVerification(token); setSent(true); } catch { /* ignore */ } finally { setBusy(false); }
  }

  return (
    <div
      className="notice"
      style={{
        display: "flex", alignItems: "center", justifyContent: "space-between", gap: "1rem",
        padding: "0.6rem 1.5rem", borderBottom: "1px solid var(--border)",
        borderRadius: 0,
        background: "var(--surface-2)",
        borderLeft: "3px solid var(--warning)",
      }}
    >
      <span className="small">
        <strong>Verify your email</strong> to post RFQs. Check your inbox for the verification link.
      </span>
      <span className="row" style={{ gap: "0.5rem" }}>
        {sent ? (
          <span className="small muted">Sent — check your inbox.</span>
        ) : (
          <button className="btn btn-sm" onClick={resend} disabled={busy}>
            {busy ? "Sending…" : "Resend email"}
          </button>
        )}
        <button className="btn btn-sm btn-ghost" aria-label="Dismiss" onClick={() => setDismissed(true)}>×</button>
      </span>
    </div>
  );
}
