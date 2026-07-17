"use client";
import { useState } from "react";
import Link from "next/link";
import { assistCompare } from "@/lib/assist";
import { PaywallError } from "@/lib/billing";

// Buyer-only "Explain these quotes" panel on the compare page: posts to
// /v1/assist/compare and renders the explanation, or an upgrade prompt if
// the buyer's monthly assist quota is capped (402).
export function AssistExplain({ rfqId, token }: { rfqId: string; token: string }) {
  const [explanation, setExplanation] = useState<string | null>(null);
  const [paywall, setPaywall] = useState<PaywallError | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const explain = async () => {
    setBusy(true);
    setErr(null);
    setPaywall(null);
    try {
      const { explanation } = await assistCompare(token, rfqId);
      setExplanation(explanation);
    } catch (e) {
      if (e instanceof PaywallError) setPaywall(e);
      else setErr(e instanceof Error ? e.message : "Couldn't explain quotes — retry");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card" style={{ marginBottom: "1.25rem" }}>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
        <div>
          <h2>Assistant</h2>
          <p className="muted small" style={{ marginTop: "0.15rem" }}>
            Get a plain-language comparison of the quotes on this RFQ.
          </p>
        </div>
        <button className="btn btn-sm" disabled={busy} onClick={explain}>
          {busy ? "Thinking…" : "Explain these quotes"}
        </button>
      </div>
      {explanation && (
        <p style={{ marginTop: "0.85rem" }}>{explanation}</p>
      )}
      {paywall && (
        <p className="muted small" style={{ marginTop: "0.85rem" }}>
          You&apos;ve used this month&apos;s AI assist quota.{" "}
          <Link href="/settings/billing">Upgrade to {paywall.planRequired} →</Link>
        </p>
      )}
      {err && <p className="err" style={{ marginTop: "0.85rem" }}>{err}</p>}
    </div>
  );
}
