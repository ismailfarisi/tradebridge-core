"use client";

import { useEffect, useState } from "react";
import AdminShell from "@/components/AdminShell";
import { useAdminToken } from "@/lib/admin";
import { getBusinessAnalytics, type BusinessSummary } from "@/lib/analytics";

export default function AdminAnalyticsPage() {
  return (
    <AdminShell>
      <AnalyticsConsole />
    </AdminShell>
  );
}

function AnalyticsConsole() {
  const token = useAdminToken();
  const [summary, setSummary] = useState<BusinessSummary | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!token) return;
    getBusinessAnalytics(token)
      // Go serializes empty slices as JSON null; normalize so the .length/.map render paths are safe.
      .then((s) => setSummary({ ...s, funnel: s.funnel ?? [], gmv_by_week: s.gmv_by_week ?? [], plan_mix: s.plan_mix ?? [] }))
      .catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  }, [token]);

  return (
    <>
      <div style={{ marginBottom: "1.25rem" }}>
        <h1 style={{ fontSize: "1.4rem" }}>Business analytics</h1>
        <p className="muted" style={{ margin: "0.25rem 0 0" }}>
          Platform-wide GMV, activation, and subscription health, computed on the fly.
        </p>
      </div>

      {err && <p className="err">{err}</p>}
      {!err && !summary && <p className="muted">Loading…</p>}

      {summary && (
        <>
          <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))", marginBottom: "1.5rem" }}>
            <Stat n={formatMoney(summary.gmv_cents)} label={`GMV (${summary.orders} orders)`} />
            <Stat n={summary.active_buyers} label="Active buyers (30d)" />
            <Stat n={summary.active_sellers} label="Active sellers (30d)" />
            <Stat
              n={summary.prices_set && summary.mrr_cents != null ? formatMoney(summary.mrr_cents) : "prices unset"}
              label="MRR"
            />
            <Stat n={`${summary.churn_pct.toFixed(1)}%`} label={`Churn (${summary.canceled_subs}/${summary.active_subs + summary.canceled_subs})`} />
          </div>

          <section style={{ marginBottom: "1.5rem" }}>
            <h2 style={{ fontSize: "1.05rem", marginBottom: "0.5rem" }}>Activation funnel</h2>
            <div className="card">
              {summary.funnel.length === 0 && <p className="muted">No activation events recorded yet.</p>}
              {summary.funnel.length > 0 && (
                <ul style={{ margin: 0, padding: 0, listStyle: "none" }}>
                  {summary.funnel.map((f) => (
                    <li
                      key={f.event}
                      className="row"
                      style={{ justifyContent: "space-between", padding: "0.4rem 0", borderBottom: "1px solid var(--border)" }}
                    >
                      <span className="small">{f.event}</span>
                      <span className="small muted">{f.companies} companies</span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </section>

          <section style={{ marginBottom: "1.5rem" }}>
            <h2 style={{ fontSize: "1.05rem", marginBottom: "0.5rem" }}>GMV by week (last 12 weeks)</h2>
            <div className="card">
              <table className="tbl">
                <thead>
                  <tr>
                    <th>Week</th>
                    <th>GMV</th>
                  </tr>
                </thead>
                <tbody>
                  {summary.gmv_by_week.map((w) => (
                    <tr key={w.week}>
                      <td className="small">{w.week}</td>
                      <td className="small">{formatMoney(w.gmv_cents)}</td>
                    </tr>
                  ))}
                  {summary.gmv_by_week.length === 0 && (
                    <tr><td colSpan={2} className="muted">No orders in the last 12 weeks.</td></tr>
                  )}
                </tbody>
              </table>
            </div>
          </section>

          <section>
            <h2 style={{ fontSize: "1.05rem", marginBottom: "0.5rem" }}>Plan mix</h2>
            <div className="card">
              <table className="tbl">
                <thead>
                  <tr>
                    <th>Plan</th>
                    <th>Status</th>
                    <th>Count</th>
                  </tr>
                </thead>
                <tbody>
                  {summary.plan_mix.map((r) => (
                    <tr key={`${r.plan}-${r.status}`}>
                      <td className="small">{r.plan}</td>
                      <td className="small">{r.status}</td>
                      <td className="small">{r.count}</td>
                    </tr>
                  ))}
                  {summary.plan_mix.length === 0 && (
                    <tr><td colSpan={3} className="muted">No subscriptions yet.</td></tr>
                  )}
                </tbody>
              </table>
            </div>
          </section>
        </>
      )}
    </>
  );
}

function formatMoney(cents: number): string {
  return (cents / 100).toLocaleString(undefined, { style: "currency", currency: "AED" });
}

function Stat({ n, label }: { n: number | string; label: string }) {
  return (
    <div className="stat">
      <div className="n">{n}</div>
      <div className="l">{label}</div>
    </div>
  );
}
