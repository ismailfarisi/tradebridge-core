"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { PageHeader } from "@/components/AppShell";
import UsageMeter from "@/components/UsageMeter";
import { useToken } from "@/lib/auth";
import { getBillingSummary, openBillingPortal, startCheckout, type BillingSummary } from "@/lib/billing";

function BillingInner() {
  const token = useToken();
  const params = useSearchParams();
  const justChecked = params.get("checkout") === "success";
  const [summary, setSummary] = useState<BillingSummary | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!token) return;
    let active = true;
    let tries = 0;
    const load = () => {
      getBillingSummary(token)
        .then((s) => {
          if (!active) return;
          setSummary(s);
          // After returning from Checkout, the webhook may lag; poll a few times.
          if (justChecked && s.plan === "free" && tries < 5) {
            tries += 1;
            setTimeout(load, 2000);
          }
        })
        .catch((e) => active && setErr(e instanceof Error ? e.message : "failed"));
    };
    load();
    return () => { active = false; };
  }, [token, justChecked]);

  const portal = async () => {
    if (!token) return;
    setBusy(true);
    try {
      window.location.href = await openBillingPortal(token);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "portal unavailable");
      setBusy(false);
    }
  };

  const upgrade = async (plan: "pro" | "business") => {
    if (!token) return;
    setBusy(true);
    try {
      window.location.href = await startCheckout(token, plan, "month");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "checkout failed");
      setBusy(false);
    }
  };

  return (
    <>
      <PageHeader title="Billing & usage" subtitle="Your plan, usage, and payment settings." />
      {justChecked && summary?.plan === "free" && (
        <div className="notice" style={{ marginBottom: "1rem" }}>Activating your subscription… this can take a few seconds.</div>
      )}
      {err && <div className="notice err" style={{ marginBottom: "1rem" }}>{err}</div>}
      {!summary ? (
        <p className="muted">Loading…</p>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: "1.25rem" }}>
          <div className="card">
            <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
              <div>
                <div className="faint small" style={{ textTransform: "uppercase", letterSpacing: "0.06em" }}>Current plan</div>
                <div style={{ fontSize: "1.5rem", fontWeight: 700, textTransform: "capitalize" }}>{summary.plan}</div>
              </div>
              <div className="row" style={{ gap: "0.5rem" }}>
                {summary.plan === "free"
                  ? <button className="btn btn-primary" disabled={busy} onClick={() => upgrade("pro")}>{busy ? "…" : "Upgrade to Pro"}</button>
                  : <button className="btn btn-ghost" disabled={busy} onClick={portal}>{busy ? "…" : "Manage in portal"}</button>}
              </div>
            </div>
          </div>

          <div className="card">
            <h2 style={{ fontSize: "1.05rem", marginBottom: "1rem" }}>Usage this month</h2>
            <div style={{ display: "flex", flexDirection: "column", gap: "1rem" }}>
              <UsageMeter label="RFQs posted" used={summary.usage.rfq_used} limit={summary.usage.rfq_limit} />
              <UsageMeter label="Catalog items" used={summary.usage.catalog_used} limit={summary.usage.catalog_limit} />
              <UsageMeter label="Team seats" used={summary.usage.seats_used} limit={summary.usage.seats_limit} />
            </div>
          </div>
        </div>
      )}
    </>
  );
}

export default function BillingPage() {
  return (
    <Suspense fallback={<p className="muted">Loading…</p>}>
      <BillingInner />
    </Suspense>
  );
}
