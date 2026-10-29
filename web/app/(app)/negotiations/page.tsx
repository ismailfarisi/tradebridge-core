"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { PageHeader } from "@/components/AppShell";
import { Icon } from "@/components/Icon";
import { NeedsYou, needsYou } from "@/components/NeedsYou";
import { useToken } from "@/lib/auth";
import { listRfqs, type RFQ } from "@/lib/api";
import {
  listNegotiations,
  listSaving,
  money,
  STATUS_LABEL,
  statusClass,
  wholeAmount,
  type NegotiationListItem,
  type NegotiationStatus,
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
  const [rfqs, setRfqs] = useState<RFQ[]>([]);
  const [filter, setFilter] = useState<"" | NegotiationStatus>("");
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!token) return;
    listNegotiations(token).then((r) => setItems(r.negotiations)).catch((e) => setErr(e.message));
    listRfqs(token).then((r) => setRfqs(r.rfqs)).catch(() => {});
  }, [token]);

  if (err) return <p className="err">{err}</p>;
  if (!items) return <p className="muted">Loading negotiations…</p>;

  const shown = filter ? items.filter((n) => n.status === filter) : items;
  const todo = needsYou(items, rfqs);

  return (
    <>
      <PageHeader
        title="Negotiations"
        subtitle="Your agent negotiates every supplier quote within the limits you set."
      />

      <NeedsYou {...todo} showEmpty={false} />

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
            <table className="tbl tbl-cards">
              <thead>
                <tr>
                  <th style={{ paddingLeft: "1.25rem" }}>RFQ</th>
                  <th>Status</th>
                  <th style={{ textAlign: "right" }}>Suppliers</th>
                  <th style={{ textAlign: "right" }} title="Counter-offers the agent sent across all suppliers">Counters</th>
                  <th style={{ textAlign: "right" }}>Target</th>
                  <th style={{ textAlign: "right" }}>Best offer</th>
                  <th style={{ textAlign: "right", paddingRight: "1.25rem" }}>Saved</th>
                </tr>
              </thead>
              <tbody>
                {shown.map((n) => (
                  <tr key={n.id}>
                    <td className="tc-main" style={{ paddingLeft: "1.25rem" }}>
                      <Link className="rowlink" href={`/rfqs/${n.rfq_id}/negotiation`}>{n.rfq_title}</Link>
                      <div className="muted small">{n.auto_accept ? "Orders automatically within your limits" : "You approve the final deal"}</div>
                    </td>
                    <td className="tc-main"><span className={statusClass(n.status)}>{STATUS_LABEL[n.status]}</span></td>
                    <td className="num tc-hide-m" style={{ textAlign: "right" }}>{n.quote_count}</td>
                    <td className="num tc-hide-m" style={{ textAlign: "right" }}>{n.round_count}</td>
                    <td className="num muted tc-hide-m" style={{ textAlign: "right" }}>{money(wholeAmount(n.target_price_cents))}</td>
                    <td className="num" data-label={n.status === "completed" ? "Agreed price" : "Best offer"} style={{ textAlign: "right", fontWeight: 600 }}>{money(n.current_best_cents || null)}</td>
                    <td className="num up" data-label="Saved" style={{ textAlign: "right", fontWeight: 600, paddingRight: "1.25rem" }}>
                      {listSaving(n) > 0 ? money(listSaving(n)) : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

    </>
  );
}
