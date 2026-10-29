"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { PageHeader } from "@/components/AppShell";
import { useMe } from "@/lib/auth";
import { getBuyerDashboard, getSellerDashboard, type BuyerDashboard, type SellerDashboard } from "@/lib/dashboard";
import { SellerAnalyticsPanel } from "@/components/SellerAnalytics";
import { ActivationChecklist } from "@/components/ActivationChecklist";
import { Icon } from "@/components/Icon";
import { NeedsYou, needsYou } from "@/components/NeedsYou";
import { listRfqs, type RFQ } from "@/lib/api";
import { getSavings, listNegotiations, money, pct, rfqStatus, type NegotiationListItem, type Savings } from "@/lib/negotiation";

export default function Dashboard() {
  const { token, me } = useMe();
  const [buyer, setBuyer] = useState<BuyerDashboard | null>(null);
  const [seller, setSeller] = useState<SellerDashboard | null>(null);
  const [savings, setSavings] = useState<Savings | null>(null);
  const [negs, setNegs] = useState<NegotiationListItem[] | null>(null);
  const [rfqs, setRfqs] = useState<RFQ[] | null>(null);

  const isSeller = me?.company_type === "seller" || me?.company_type === "both";
  const isBuyer = me ? me.company_type !== "seller" : false;

  useEffect(() => {
    if (!token || !me) return;
    if (isBuyer) getBuyerDashboard(token).then(setBuyer).catch(() => {});
    if (isBuyer) getSavings(token).then(setSavings).catch(() => {});
    if (isBuyer) listNegotiations(token).then((r) => setNegs(r.negotiations)).catch(() => setNegs([]));
    if (isBuyer) listRfqs(token).then((r) => setRfqs(r.rfqs)).catch(() => setRfqs([]));
    if (isSeller) getSellerDashboard(token).then(setSeller).catch(() => {});
  }, [token, me, isBuyer, isSeller]);

  if (!me) return null;

  const todo = isBuyer && negs && rfqs ? needsYou(negs, rfqs) : null;
  const todoCount = todo ? todo.approvals.length + todo.unmanaged.length : 0;
  const subtitle = !todo
    ? isBuyer ? " " : "Your marketplace at a glance."
    : todoCount
      ? `${todoCount} decision${todoCount === 1 ? "" : "s"} waiting for you.`
      : "Nothing needs you right now.";

  return (
    <>
      <PageHeader title={`Welcome, ${me.company_name ?? "there"}`} subtitle={subtitle} />
      <ActivationChecklist />

      {todo && <NeedsYou {...todo} />}

      {isBuyer && savings && savings.totals.deals > 0 && (
        <Link href="/savings" className="card savings-strip">
          <Icon name="trending" size={18} />
          <span className="grow">
            Your agent has saved <span className="amt">{money(savings.totals.savings_cents)}</span>{" "}
            <span className="small muted">{pct(savings.savings_pct)} below the cheapest opening quotes, across {savings.totals.deals} deal{savings.totals.deals === 1 ? "" : "s"}</span>
          </span>
          <span className="small row" style={{ display: "inline-flex", gap: "0.3rem", color: "var(--primary)" }}>Savings report <Icon name="arrowRight" size={14} /></span>
        </Link>
      )}

      {isBuyer && buyer && (
        <section>
          <div className="stats3">
            <Stat n={buyer.active_rfqs} label="Active RFQs" />
            <Stat n={buyer.quotes_received} label="Quotes received" badge={buyer.new_quotes < buyer.quotes_received ? buyer.new_quotes : 0} />
            <Stat n={buyer.orders_in_progress} label="Orders in progress" />
          </div>
          <div className="card">
            <h2 style={{ marginTop: 0 }}>Recent RFQs</h2>
            {buyer.recent_rfqs.length === 0 ? <p className="muted">No RFQs yet.</p> : (
              <table className="tbl tbl-cards">
                <thead>
                  <tr><th>RFQ</th><th>Status</th><th style={{ textAlign: "right" }}>Quotes</th></tr>
                </thead>
                <tbody>
                  {buyer.recent_rfqs.map((r) => {
                    const st = rfqStatus(r.status);
                    return (
                      <tr key={r.id}>
                        <td className="tc-main"><Link className="rowlink" href={`/rfqs/${r.id}`}>{r.title}</Link></td>
                        <td><span className={st.cls}>{st.label}</span></td>
                        <td className="num" style={{ textAlign: "right" }}>
                          {r.quote_count} quote{r.quote_count === 1 ? "" : "s"}{r.new_quotes > 0 ? ` · ${r.new_quotes} new` : ""}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
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
