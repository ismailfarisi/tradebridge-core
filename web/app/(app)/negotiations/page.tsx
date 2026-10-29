"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { PageHeader } from "@/components/AppShell";
import { Icon } from "@/components/Icon";
import { useToken } from "@/lib/auth";
import { listRfqs, type RFQ } from "@/lib/api";
import {
  getSavings,
  listNegotiations,
  money,
  pct,
  STATUS_LABEL,
  statusClass,
  type NegotiationListItem,
  type NegotiationStatus,
  type Savings,
} from "@/lib/negotiation";

const FILTERS: { key: "" | NegotiationStatus; label: string }[] = [
  { key: "", label: "All" },
  { key: "active", label: "Live" },
  { key: "awaiting_approval", label: "Needs approval" },
  { key: "completed", label: "Closed" },
  { key: "cancelled", label: "No deal" },
];

export default function NegotiationsPage() {
  const token = useToken();
  const [items, setItems] = useState<NegotiationListItem[] | null>(null);
  const [savings, setSavings] = useState<Savings | null>(null);
  const [rfqs, setRfqs] = useState<RFQ[]>([]);
  const [filter, setFilter] = useState<"" | NegotiationStatus>("");
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!token) return;
    listNegotiations(token).then((r) => setItems(r.negotiations)).catch((e) => setErr(e.message));
    getSavings(token).then(setSavings).catch(() => {});
    listRfqs(token).then((r) => setRfqs(r.rfqs)).catch(() => {});
  }, [token]);

  const unmanaged = useMemo(() => {
    const managed = new Set((items ?? []).map((n) => n.rfq_id));
    return rfqs.filter((r) => !managed.has(r.id) && r.status === "quoted");
  }, [items, rfqs]);

  if (err) return <p className="err">{err}</p>;
  if (!items) return <p className="muted">Loading negotiations…</p>;

  const shown = filter ? items.filter((n) => n.status === filter) : items;
  const approvals = items.filter((n) => n.status === "awaiting_approval");

  return (
    <>
      <PageHeader
        title="Negotiations"
        subtitle="Your agent negotiates every supplier quote within the limits you set."
        action={<Link href="/chat" className="btn btn-primary"><Icon name="plus" size={16} /> New RFQ</Link>}
      />

      {savings && (
        <div className="kpis">
          <div className="kpi kpi-accent">
            <div className="kpi-label"><Icon name="trending" size={16} /> Saved to date</div>
            <div className="kpi-value">{money(savings.totals.savings_cents)}</div>
            <div className="kpi-sub">{pct(savings.savings_pct)} off opening quotes</div>
          </div>
          <div className="kpi">
            <div className="kpi-label"><Icon name="zap" size={16} /> Live</div>
            <div className="kpi-value">{savings.negotiations.active}</div>
            <div className="kpi-sub">negotiating now</div>
          </div>
          <div className="kpi">
            <div className="kpi-label"><Icon name="clock" size={16} /> Needs approval</div>
            <div className="kpi-value">{savings.negotiations.awaiting_approval}</div>
            <div className="kpi-sub">best deals waiting for you</div>
          </div>
          <div className="kpi">
            <div className="kpi-label"><Icon name="checkCircle" size={16} /> Closed</div>
            <div className="kpi-value">{savings.negotiations.completed}</div>
            <div className="kpi-sub">{savings.negotiations.avg_rounds.toFixed(1)} rounds on average</div>
          </div>
        </div>
      )}

      {approvals.length > 0 && (
        <div className="banner banner-warn">
          <Icon name="clock" />
          <div className="grow">
            <strong>{approvals.length} deal{approvals.length === 1 ? "" : "s"} ready for your approval.</strong>{" "}
            <span className="small">Your agent finished negotiating and is holding the best offer.</span>
          </div>
          <Link className="btn btn-sm" href={`/rfqs/${approvals[0].rfq_id}/negotiation`}>Review</Link>
        </div>
      )}

      <section className="card" style={{ padding: 0 }}>
        <div className="section-head" style={{ padding: "1rem 1.25rem 0" }}>
          <div className="tabs" role="tablist">
            {FILTERS.map((f) => (
              <button key={f.key} role="tab" aria-selected={filter === f.key} className={`tab${filter === f.key ? " on" : ""}`} onClick={() => setFilter(f.key)}>
                {f.label}
              </button>
            ))}
          </div>
        </div>
        {shown.length === 0 ? (
          <div className="empty">
            <Icon name="handshake" size={30} />
            <p style={{ margin: 0 }}>{items.length ? "Nothing in this view." : "No negotiations yet. Open an RFQ with quotes and hand it to your agent."}</p>
          </div>
        ) : (
          <div style={{ overflowX: "auto" }}>
            <table className="tbl">
              <thead>
                <tr>
                  <th style={{ paddingLeft: "1.25rem" }}>RFQ</th>
                  <th>Status</th>
                  <th style={{ textAlign: "right" }}>Suppliers</th>
                  <th style={{ textAlign: "right" }}>Rounds</th>
                  <th style={{ textAlign: "right" }}>Target</th>
                  <th style={{ textAlign: "right" }}>Best offer</th>
                  <th style={{ textAlign: "right", paddingRight: "1.25rem" }}>Saved</th>
                </tr>
              </thead>
              <tbody>
                {shown.map((n) => (
                  <tr key={n.id}>
                    <td style={{ paddingLeft: "1.25rem" }}>
                      <Link className="rowlink" href={`/rfqs/${n.rfq_id}/negotiation`}>{n.rfq_title}</Link>
                      <div className="faint small">{n.auto_accept ? "Auto-accept" : "Approval required"} · {n.tone}</div>
                    </td>
                    <td><span className={statusClass(n.status)}>{STATUS_LABEL[n.status]}</span></td>
                    <td className="num" style={{ textAlign: "right" }}>{n.quote_count}</td>
                    <td className="num" style={{ textAlign: "right" }}>{n.round_count}</td>
                    <td className="num muted" style={{ textAlign: "right" }}>{money(n.target_price_cents)}</td>
                    <td className="num" style={{ textAlign: "right", fontWeight: 600 }}>{money(n.current_best_cents || null)}</td>
                    <td className="num up" style={{ textAlign: "right", fontWeight: 600, paddingRight: "1.25rem" }}>
                      {n.savings_cents > 0 ? money(n.savings_cents) : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {unmanaged.length > 0 && (
        <section className="card" style={{ marginTop: "1rem" }}>
          <div className="section-head">
            <h2>Quoted RFQs without an agent</h2>
            <span className="small muted">Set a target and let the agent negotiate</span>
          </div>
          <div className="col" style={{ gap: "0.5rem" }}>
            {unmanaged.slice(0, 6).map((r) => (
              <div key={r.id} className="row" style={{ justifyContent: "space-between", padding: "0.5rem 0", borderTop: "1px solid var(--border)" }}>
                <span><strong>{r.title}</strong> <span className="faint small">· {r.match_count} matched</span></span>
                <Link className="btn btn-sm btn-primary" href={`/rfqs/${r.id}/negotiation`}><Icon name="zap" size={14} /> Negotiate</Link>
              </div>
            ))}
          </div>
        </section>
      )}
    </>
  );
}
