"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { PageHeader } from "@/components/AppShell";
import { Icon } from "@/components/Icon";
import { SpendChart } from "@/components/SpendChart";
import { useToken } from "@/lib/auth";
import { BASIS_LABEL, DEFAULT_CURRENCY, getSavings, money, pct, shortDate, verifiedHeadline, type Savings } from "@/lib/negotiation";

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
  const v = data.verified;
  const head = verifiedHeadline(data);
  const cur = head.currency;
  const chart = data.monthly.map((m) => ({ ...m, savings_cents: m.verified_savings_cents ?? m.savings_cents }));
  const maxSupplier = Math.max(1, ...data.top_suppliers.map((s) => s.spend_cents));

  return (
    <>
      <PageHeader
        title="Savings"
        subtitle="What your negotiation agent has saved, measured against the lowest fair reference for each deal."
        action={<Link href="/negotiations" className="btn"><Icon name="handshake" size={16} /> All negotiations</Link>}
      />

      <div className="kpis">
        <div className="kpi kpi-accent">
          <div className="kpi-label"><Icon name="trending" size={16} /> {head.verified ? "Verified savings" : "Total savings"}</div>
          <div className="kpi-value">{money(head.cents, cur)}</div>
          <div className="kpi-sub">
            {head.verified ? `${pct(head.pct)} below the lowest fair reference` : `${pct(head.pct)} below the cheapest opening quotes`}
          </div>
          {v && (
            <div className="kpi-sub faint">Vs cheapest opening quotes: {money(t.savings_cents, cur)}</div>
          )}
        </div>
        <div className="kpi">
          <div className="kpi-label"><Icon name="dollar" size={16} /> Negotiated spend</div>
          <div className="kpi-value">{money(t.spend_cents)}</div>
          <div className="kpi-sub">across {t.deals} deal{t.deals === 1 ? "" : "s"}</div>
        </div>
</div>

      {v && (
        <section className="card" style={{ marginBottom: "1rem" }} aria-labelledby="how-measured">
          <div className="section-head"><h2 id="how-measured">How savings are measured</h2></div>
          <p className="small muted" style={{ marginTop: 0 }}>{v.explanation}</p>
          <p className="small faint" style={{ margin: 0 }}>
            {v.by_basis.market} vs typical market price · {v.by_basis.historical} vs your last price · {v.by_basis.opening_quote} vs opening quote
            {v.by_basis.none > 0 && ` · ${v.by_basis.none} with no reference`}
            {v.anchored_deals > 0 && ` · ${v.anchored_deals} inflated opening${v.anchored_deals === 1 ? "" : "s"} set aside`}
          </p>
        </section>
      )}

      <div className="split" style={{ marginBottom: "1rem" }}>
        <section className="card">
          <div className="section-head"><h2>Spend and {v ? "verified " : ""}savings, last 12 months</h2></div>
          <SpendChart data={chart} currency={cur} savingsLabel={v ? "Verified savings" : "Savings vs cheapest opening quotes"} />
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
                <tr><th>RFQ</th><th>Supplier</th><th style={{ textAlign: "right" }} title="Cheapest opening quote from any supplier">Best opening</th><th style={{ textAlign: "right" }}>Final</th><th style={{ textAlign: "right" }}>Verified saved</th><th style={{ textAlign: "right" }} title="Saved against the cheapest opening quote from any supplier">Vs best opening</th><th style={{ textAlign: "right" }} title="Counter-offers the agent sent across all suppliers">Counters</th><th>Date</th></tr>
              </thead>
              <tbody>
                {data.recent_deals.map((d) => {
                  const saved = Math.max(0, d.initial_cents - d.total_cents);
                  const vs = d.verified_savings_cents ?? 0;
                  return (
                    <tr key={d.order_id}>
                      <td className="tc-main"><Link className="rowlink" href={`/rfqs/${d.rfq_id}/negotiation`}>{d.rfq_title}</Link></td>
                      <td className="tc-main tc-sub">{d.seller_name}</td>
                      <td className="num muted tc-hide-m" style={{ textAlign: "right" }}>{money(d.initial_cents, d.currency)}</td>
                      <td className="num" data-label="Final price" style={{ textAlign: "right", fontWeight: 600 }}>{money(d.total_cents, d.currency)}</td>
                      <td className="num" data-label="Verified saved" style={{ textAlign: "right" }} title={d.basis_reason || undefined}>
                        <span className={vs > 0 ? "up" : "faint"} style={{ fontWeight: 600 }}>{vs > 0 ? money(vs, d.currency) : "—"}</span>
                        {d.verified_basis && <div className="faint small">vs {BASIS_LABEL[d.verified_basis]}</div>}
                        {d.anchored && <div><span className="badge badge-warning" title={d.basis_reason}>Inflated opening</span></div>}
                      </td>
                      <td className="num muted tc-hide-m" style={{ textAlign: "right" }}>
                        {saved > 0 ? `${money(saved, d.currency)} (${pct((saved / d.initial_cents) * 100)})` : "—"}
                        <div className="faint small">from {money(d.initial_cents, d.currency)}</div>
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
