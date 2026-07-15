"use client";

import { useEffect, useRef, useState } from "react";
import { useParams } from "next/navigation";
import { useToken } from "@/lib/auth";
import { acceptQuote, getRfq, rfqQuotes, type QuoteRow, type RFQ } from "@/lib/api";
import { AttachmentView } from "@/components/AttachmentView";

const POLL_MS = 4000;

export default function ChatThread() {
  const token = useToken();
  const { rfqId } = useParams<{ rfqId: string }>();
  const [rfq, setRfq] = useState<RFQ | null>(null);
  const [quotes, setQuotes] = useState<QuoteRow[]>([]);
  const [msg, setMsg] = useState<string | null>(null);
  const [msgKind, setMsgKind] = useState<"ok" | "err">("ok");
  const [busy, setBusy] = useState<string | null>(null);
  const closedRef = useRef(false);

  useEffect(() => {
    if (!token || !rfqId) return;
    getRfq(token, rfqId).then((r) => {
      setRfq(r.rfq);
      closedRef.current = r.rfq.status === "closed";
    }).catch(() => {});
  }, [token, rfqId]);

  useEffect(() => {
    if (!token || !rfqId) return;
    let active = true;
    const poll = () =>
      rfqQuotes(token, rfqId).then((r) => active && setQuotes(r.quotes)).catch(() => {});
    poll();
    const t = setInterval(() => {
      if (closedRef.current) return;
      poll();
    }, POLL_MS);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, [token, rfqId]);

  const accept = async (quoteId: string) => {
    if (!token) return;
    setBusy(quoteId);
    try {
      const { order_id, total_cents } = await acceptQuote(token, quoteId);
      setMsg(`✅ Order created — ${order_id} for $${(total_cents / 100).toFixed(2)}.`);
      setMsgKind("ok");
      closedRef.current = true;
      const r = await getRfq(token, rfqId);
      setRfq(r.rfq);
      const q = await rfqQuotes(token, rfqId);
      setQuotes(q.quotes);
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "accept failed");
      setMsgKind("err");
    } finally {
      setBusy(null);
    }
  };

  if (!rfq) return <div style={{ padding: "1.5rem" }} className="muted">Loading…</div>;

  const closed = rfq.status === "closed";

  return (
    <div style={{ flex: 1, overflowY: "auto", padding: "1.5rem", maxWidth: 760, width: "100%", margin: "0 auto" }}>
      <div className="col" style={{ gap: "0.75rem" }}>
        {(rfq.messages ?? []).map((m, i) => (
          <div key={i} style={{ display: "flex", justifyContent: m.role === "user" ? "flex-end" : "flex-start" }}>
            <div className="card" style={{ maxWidth: "80%", background: m.role === "user" ? "var(--surface-2)" : "var(--surface)", padding: "0.6rem 0.85rem" }}>
              {m.content}
              {m.attachments?.map((a) => (
                <div key={a.id} style={{ marginTop: "0.35rem" }}>
                  <AttachmentView meta={a} token={token ?? ""} />
                </div>
              ))}
            </div>
          </div>
        ))}

        <div className="card">
          <div className="row" style={{ justifyContent: "space-between" }}>
            <strong>Quotes</strong>
            <span className="badge">{rfq.status}</span>
          </div>
          {quotes.length === 0 ? (
            <p className="muted small" style={{ marginTop: "0.5rem" }}>Sellers&apos; agents are responding…</p>
          ) : (
            <div className="col" style={{ gap: "0.5rem", marginTop: "0.6rem" }}>
              {quotes.map((q, i) => (
                <div key={q.id} className="row" style={{ justifyContent: "space-between", alignItems: "center", borderTop: i ? "1px solid var(--border)" : "none", paddingTop: i ? "0.5rem" : 0 }}>
                  <div style={{ minWidth: 0 }}>
                    <strong>{q.currency} {(q.price_cents / 100).toFixed(2)}</strong>
                    <span className="muted small"> · {q.seller_name ?? q.seller_company_id.slice(0, 8)} · {q.delivery_days != null ? `${q.delivery_days}d` : "—"}</span>
                    {q.notes && <div className="muted small">{q.notes}</div>}
                  </div>
                  {q.status === "accepted" ? (
                    <span className="badge badge-success">accepted</span>
                  ) : q.status === "rejected" ? (
                    <span className="badge">rejected</span>
                  ) : !closed ? (
                    <button className="btn btn-sm btn-primary" disabled={busy === q.id} onClick={() => accept(q.id)}>
                      {busy === q.id ? "…" : "Accept"}
                    </button>
                  ) : null}
                </div>
              ))}
            </div>
          )}
        </div>

        {msg && <p className={msgKind}>{msg}</p>}
      </div>
    </div>
  );
}
