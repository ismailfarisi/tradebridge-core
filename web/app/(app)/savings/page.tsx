"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { PageHeader } from "@/components/AppShell";
import { Icon } from "@/components/Icon";
import { SpendChart } from "@/components/SpendChart";
import { useToken } from "@/lib/auth";
import { DEFAULT_CURRENCY, getSavings, money, pct, shortDate, type Savings } from "@/lib/negotiation";

export default function SavingsPage() {
  const token = useToken();
  const [data, setData] = useState<Savings | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!token) return;
    getSavings(token).then(setData).catch((e) => setErr(e.message));
  }, [token]);

  if (err) return <p className="err">{err}</p>;
  if (!data) return <p className="muted">Loading savings…</p>;

  const t = data.totals;
  const maxSupplier = Math.max(1, ...data.top_suppliers.map((s) => s.spend_cents));

  return (
    <>
      <PageHeader
        title="Savings"
        subtitle="What your negotiation agent has saved against opening supplier quotes."
        action={<Link href="/negotiations" className="btn"><Icon name="handshake" size={16} /> All negotiations</Link>}
      />

      <div className="kpis">
        <div className="kpi kpi-accent">
          <div className="kpi-label"><Icon name="trending" size={16} /> Total savings</div>
          <div className="kpi-value">{money(t.savings_cents)}</div>
          <div className="kpi-sub">{pct(data.savings_pct)} below opening quotes</div>
        </div>
        <div className="kpi">
          <div className="kpi-label"><Icon name="dollar" size={16} /> Negotiated spend</div>
          <div className="kpi-value">{money(t.spend_cents)}</div>
          <div className="kpi-sub">across {t.deals} deal{t.deals === 1 ? "" : "s"}</div>
        </div>
</div>

      <div className="split" style={{ marginBottom: "1rem" }}>
        <section className="card">
          <div className="section-head"><h2>Spend and savings, last 12 months</h2></div>
          <SpendChart data={data.monthly} />
        </section>
        <section className="card">
          <div className="section-head"><h2>Top suppliers by spend</h2></div>
          {data.top_suppliers.length === 0 ? (
            <p className="muted small">No awarded deals yet.</p>
          ) : (
            <div className="col" style={{ gap: "0.9rem" }}>
              {data.top_suppliers.map((s) => (
                <div key={s.id}>
                  <div className="row small" style={{ justifyContent: "space-between" }}>
                    <strong>{s.name}</strong>
                    <span className="num muted">{money(s.spend_cents, DEFAULT_CURRENCY, { compact: true })} · {s.deals} deal{s.deals === 1 ? "" : "s"}</span>
                  </div>
                  <div className="bar" style={{ marginTop: "0.35rem" }}><span style={{ width: `${(s.spend_cents / maxSupplier) * 100}%` }} /></div>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>

      <section className="card">
        <div className="section-head"><h2>Recent deals</h2></div>
        {data.recent_deals.length === 0 ? (
          <div className="empty">
            <Icon name="handshake" size={28} />
            <p style={{ margin: 0 }}>No deals yet. Give an RFQ to your negotiation agent to start saving.</p>
          </div>
        ) : (
          <div style={{ overflowX: "auto" }}>
            <table className="tbl tbl-cards">
              <thead>
                <tr><th>RFQ</th><th>Supplier</th><th style={{ textAlign: "right" }}>Opening</th><th style={{ textAlign: "right" }}>Final</th><th style={{ textAlign: "right" }}>Saved</th><th style={{ textAlign: "right" }} title="Counter-offers the agent sent across all suppliers">Counters</th><th>Date</th></tr>
              </thead>
              <tbody>
                {data.recent_deals.map((d) => {
                  const saved = Math.max(0, d.initial_cents - d.total_cents);
                  return (
                    <tr key={d.order_id}>
                      <td className="tc-main"><Link className="rowlink" href={`/rfqs/${d.rfq_id}/negotiation`}>{d.rfq_title}</Link></td>
                      <td className="tc-main tc-sub">{d.seller_name}</td>
                      <td className="num muted tc-hide-m" style={{ textAlign: "right" }}>{money(d.initial_cents, d.currency)}</td>
                      <td className="num" data-label="Final price" style={{ textAlign: "right", fontWeight: 600 }}>{money(d.total_cents, d.currency)}</td>
                      <td className="num up" data-label="Saved" style={{ textAlign: "right", fontWeight: 600 }}>
                        {saved > 0 ? `${money(saved, d.currency)} (${pct((saved / d.initial_cents) * 100)})` : "—"}
                      </td>
                      <td className="num tc-hide-m" style={{ textAlign: "right" }}>{d.rounds}</td>
                      <td className="muted tc-hide-m" style={{ whiteSpace: "nowrap" }}>{shortDate(d.created_at)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </>
  );
}
