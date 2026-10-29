"use client";

import Link from "next/link";
import { Icon } from "@/components/Icon";
import type { RFQ } from "@/lib/api";
import { money, type NegotiationListItem } from "@/lib/negotiation";

export type NeedsYouItems = { approvals: NegotiationListItem[]; unmanaged: RFQ[] };

// needsYou picks the owner's open decisions: deals held for approval, then
// RFQs whose quotes are in but have no agent negotiating yet.
export function needsYou(items: NegotiationListItem[], rfqs: RFQ[]): NeedsYouItems {
  const managed = new Set(items.map((n) => n.rfq_id));
  return {
    approvals: items.filter((n) => n.status === "awaiting_approval"),
    unmanaged: rfqs.filter((r) => !managed.has(r.id) && r.status === "quoted"),
  };
}

export function NeedsYou({ approvals, unmanaged, limit = 6, showEmpty = true }: NeedsYouItems & { limit?: number; showEmpty?: boolean }) {
  const count = approvals.length + unmanaged.length;
  if (count === 0) {
    if (!showEmpty) return null;
    return (
      <section className="card needs-you needs-done">
        <Icon name="checkCircle" size={18} />
        <span><strong style={{ color: "var(--text)" }}>Nothing needs you right now.</strong> <span className="small">Deals ready for your approval will show up here.</span></span>
      </section>
    );
  }
  const rest = unmanaged.slice(0, limit);
  return (
    <section className="card needs-you" aria-labelledby="needs-you-h">
      <div className="section-head">
        <h2 id="needs-you-h">Needs you</h2>
        <span className="small muted">{count} decision{count === 1 ? "" : "s"}</span>
      </div>
      <ul className="needs-list">
        {approvals.map((n) => (
          <li key={n.id}>
            <span className="badge sc badge-warning">Approve</span>
            <div className="grow" style={{ minWidth: 0 }}>
              <strong>{n.rfq_title}</strong>
              <div className="small muted">
                Best offer {money(n.current_best_cents || null)}
                {n.savings_cents > 0 ? ` · ${money(n.savings_cents)} below the opening quote` : ""}
              </div>
            </div>
            <Link className="btn btn-sm btn-primary" href={`/rfqs/${n.rfq_id}/negotiation`}>Review deal <Icon name="arrowRight" size={14} /></Link>
          </li>
        ))}
        {rest.map((r) => (
          <li key={r.id}>
            <span className="badge sc badge-info">Quotes in</span>
            <div className="grow" style={{ minWidth: 0 }}>
              <strong>{r.title}</strong>
              <div className="small muted">Suppliers have quoted. Tell the agent your limits and it negotiates for you.</div>
            </div>
            <Link className="btn btn-sm" href={`/rfqs/${r.id}/negotiation`}><Icon name="zap" size={14} /> Start agent</Link>
          </li>
        ))}
      </ul>
      {unmanaged.length > rest.length && (
        <p className="small muted" style={{ margin: "0.5rem 0 0" }}>+{unmanaged.length - rest.length} more RFQs with quotes on <Link href="/rfqs">your RFQs</Link>.</p>
      )}
    </section>
  );
}
