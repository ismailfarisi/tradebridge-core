"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { PageHeader } from "@/components/AppShell";
import { useMe } from "@/lib/auth";
import { getBuyerDashboard, getSellerDashboard, type BuyerDashboard, type SellerDashboard } from "@/lib/dashboard";
import { SellerAnalyticsPanel } from "@/components/SellerAnalytics";
import { ActivationChecklist } from "@/components/ActivationChecklist";
import { Icon } from "@/components/Icon";
import { SpendChart } from "@/components/SpendChart";
import { getSavings, money, pct, type Savings } from "@/lib/negotiation";

export default function Dashboard() {
  const { token, me } = useMe();
  const [buyer, setBuyer] = useState<BuyerDashboard | null>(null);
  const [seller, setSeller] = useState<SellerDashboard | null>(null);
  const [savings, setSavings] = useState<Savings | null>(null);

  const isSeller = me?.company_type === "seller" || me?.company_type === "both";
  const isBuyer = me ? me.company_type !== "seller" : false;

  useEffect(() => {
    if (!token || !me) return;
    if (isBuyer) getBuyerDashboard(token).then(setBuyer).catch(() => {});
    if (isBuyer) getSavings(token).then(setSavings).catch(() => {});
    if (isSeller) getSellerDashboard(token).then(setSeller).catch(() => {});
  }, [token, me, isBuyer, isSeller]);

  if (!me) return null;

  return (
    <>
      <PageHeader title={`Welcome, ${me.company_name ?? "there"}`} subtitle="Your marketplace at a glance." />
      <ActivationChecklist />

      {isBuyer && savings && (savings.totals.deals > 0 || savings.negotiations.active > 0) && (
        <section className="split" style={{ marginBottom: "1rem" }}>
          <div className="card">
            <div className="section-head">
              <h2>Negotiated spend</h2>
              <Link href="/savings" className="small row" style={{ display: "inline-flex", gap: "0.3rem" }}>Savings report <Icon name="arrowRight" size={14} /></Link>
            </div>
            <SpendChart data={savings.monthly} height={190} />
          </div>
          <div className="col" style={{ gap: "1rem" }}>
            <div className="kpi kpi-accent">
              <div className="kpi-label"><Icon name="trending" size={16} /> Saved by your agent</div>
              <div className="kpi-value">{money(savings.totals.savings_cents)}</div>
              <div className="kpi-sub">{pct(savings.savings_pct)} below opening quotes · {savings.totals.deals} deals</div>
            </div>
            <Link href="/negotiations" className="kpi" style={{ color: "inherit" }}>
              <div className="kpi-label"><Icon name="handshake" size={16} /> Negotiations</div>
              <div className="kpi-value">{savings.negotiations.active} live</div>
              <div className="kpi-sub">{savings.negotiations.awaiting_approval} awaiting your approval</div>
            </Link>
          </div>
        </section>
      )}

      {isBuyer && buyer && (
        <section>
          <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))", marginBottom: "1rem" }}>
            <Stat n={buyer.active_rfqs} label="Active RFQs" />
            <Stat n={buyer.quotes_received} label="Quotes received" badge={buyer.new_quotes} />
            <Stat n={buyer.orders_in_progress} label="Orders in progress" />
          </div>
          <div className="card">
            <h2 style={{ marginTop: 0 }}>Recent RFQs</h2>
            {buyer.recent_rfqs.length === 0 ? <p className="muted">No RFQs yet.</p> : (
              <table className="tbl"><tbody>
                {buyer.recent_rfqs.map((r) => (
                  <tr key={r.id}>
                    <td><Link className="rowlink" href={`/rfqs/${r.id}`}>{r.title}</Link></td>
                    <td><span className="badge">{r.status}</span></td>
                    <td>{r.quote_count} quote{r.quote_count === 1 ? "" : "s"}{r.new_quotes > 0 ? ` · ${r.new_quotes} new` : ""}</td>
                  </tr>
                ))}
              </tbody></table>
            )}
          </div>
        </section>
      )}

      {isSeller && seller && (
        <section style={{ marginTop: isBuyer ? "1.5rem" : 0 }}>
          <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))", marginBottom: "1rem" }}>
            <Stat n={seller.incoming_rfqs} label="Incoming RFQs" />
            <Stat n={seller.review_queue} label="Review queue" />
            <Stat n={seller.active_orders} label="Active orders" />
            <div className="stat">
              <div className="n" style={{ color: seller.agent.online ? "var(--success)" : "var(--muted)" }}>
                {seller.agent.online ? "Online" : "Offline"}
              </div>
              <div className="l">Agent{seller.agent.autonomy ? " · autonomous" : ""}</div>
            </div>
          </div>
          <div className="card">
            <h2 style={{ marginTop: 0 }}>Plan usage — {seller.plan_usage.plan}</h2>
            <p className="small">RFQs: {usage(seller.plan_usage.rfq_used, seller.plan_usage.rfq_limit)} · Catalog: {usage(seller.plan_usage.catalog_used, seller.plan_usage.catalog_limit)} · Seats: {usage(seller.plan_usage.seats_used, seller.plan_usage.seats_limit)}</p>
          </div>
          {token && <SellerAnalyticsPanel token={token} />}
        </section>
      )}
    </>
  );
}

function usage(used: number, limit: number): string {
  return limit < 0 ? `${used} / ∞` : `${used} / ${limit}`;
}

function Stat({ n, label, badge }: { n: number | string; label: string; badge?: number }) {
  return (
    <div className="stat">
      <div className="n">{n}{badge && badge > 0 ? <span className="badge" style={{ marginLeft: "0.4rem" }}>{badge} new</span> : null}</div>
      <div className="l">{label}</div>
    </div>
  );
}
