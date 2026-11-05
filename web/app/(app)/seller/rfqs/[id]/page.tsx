"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { PageHeader } from "@/components/AppShell";
import { AttachmentView } from "@/components/AttachmentView";
import { QuoteThread } from "@/components/QuoteThread";
import { SellerQuoteForm } from "@/components/SellerQuoteForm";
import { useToken } from "@/lib/auth";
import type { AttachmentMeta } from "@/lib/api";
import { money, when } from "@/lib/negotiation";
import { daysLeft, getSellerRfq, sellerState, type SellerRFQ } from "@/lib/seller";
import { paymentTermsLabel } from "@/lib/terms";

export default function SellerRfqDetailPage() {
  const token = useToken();
  const { id } = useParams<{ id: string }>();
  const [rfq, setRfq] = useState<SellerRFQ | null>(null);
  const [atts, setAtts] = useState<AttachmentMeta[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const [editing, setEditing] = useState(false);
  const [saved, setSaved] = useState<string | null>(null);

  useEffect(() => {
    if (!token || !id) return;
    let alive = true;
    getSellerRfq(token, id)
      .then((r) => {
        if (!alive) return;
        setRfq(r.rfq);
        setAtts(r.attachments ?? []);
      })
      .catch((e) => alive && setErr(e instanceof Error ? e.message : "failed to load RFQ"));
    return () => {
      alive = false;
    };
  }, [token, id, reload]);

  const back = <Link href="/seller/rfqs" className="btn btn-sm">← RFQ inbox</Link>;

  if (err) {
    return (
      <>
        <PageHeader title="RFQ not available" action={back} />
        <div className="card">
          <p className="err" style={{ marginTop: 0 }}>{err}</p>
          <p className="muted" style={{ marginBottom: 0 }}>
            You can only open RFQs that Core matched to your company.
          </p>
        </div>
      </>
    );
  }
  if (!rfq || !token) return <p className="muted">Loading…</p>;

  const state = sellerState(rfq);
  const live = rfq.status === "open" || rfq.status === "quoted";
  const left = live ? daysLeft(rfq.expires_at) : null;
  const req = rfq.requirements;
  const facts: [string, React.ReactNode][] = [
    ["Category", rfq.category || req.category],
    ["Quantity", req.quantity],
    ["Deliver to", req.location],
    ["Needed by", req.deadline],
    ["Quotes close", rfq.expires_at ? `${when(rfq.expires_at)}${left !== null ? ` (${left} day${left === 1 ? "" : "s"} left)` : ""}` : null],
    ["Posted", when(rfq.created_at)],
  ];
  const specs = req.specs?.filter(Boolean) ?? [];
  const keywords = req.keywords?.filter(Boolean) ?? [];

  return (
    <>
      <PageHeader
        title={rfq.title}
        subtitle={<><span className={state.cls}>{state.label}</span> <span className="muted small">{rfq.match_reason}</span></>}
        action={back}
      />

      <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(min(100%,320px),1fr))", gap: "1rem", alignItems: "start" }}>
        <div className="col" style={{ gap: "1rem", minWidth: 0 }}>
          <div className="card">
            <h2 style={{ marginTop: 0 }}>What the buyer needs</h2>
            <p style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{rfq.raw_text}</p>
            <dl style={{ display: "grid", gridTemplateColumns: "max-content 1fr", gap: "0.35rem 1rem", margin: 0 }}>
              {facts.filter(([, v]) => Boolean(v)).map(([k, v]) => (
                <div key={k} style={{ display: "contents" }}>
                  <dt className="muted small">{k}</dt>
                  <dd style={{ margin: 0 }}>{v}</dd>
                </div>
              ))}
            </dl>
            {specs.length > 0 && (
              <>
                <h3 style={{ marginBottom: "0.4rem" }}>Specifications</h3>
                <ul style={{ margin: 0, paddingLeft: "1.2rem" }}>
                  {specs.map((s) => <li key={s}>{s}</li>)}
                </ul>
              </>
            )}
            {keywords.length > 0 && (
              <div className="row wrap" style={{ gap: "0.35rem", marginTop: "0.9rem" }}>
                {keywords.map((k) => <span key={k} className="badge">{k}</span>)}
              </div>
            )}
          </div>

          {atts.length > 0 && (
            <div className="card">
              <h2 style={{ marginTop: 0 }}>Buyer attachments</h2>
              <div className="col" style={{ gap: "0.5rem" }}>
                {atts.map((a) => <AttachmentView key={a.id} meta={a} token={token} />)}
              </div>
            </div>
          )}
        </div>

        <div className="col" style={{ gap: "1rem", minWidth: 0 }}>
          <div className="card">
            <h2 style={{ marginTop: 0 }}>Your quote</h2>
            {saved && <p className="ok" role="status" style={{ marginTop: 0 }}>{saved}</p>}
            {rfq.quote && editing && live ? (
              <SellerQuoteForm
                token={token}
                rfqId={rfq.id}
                existing={rfq.quote}
                onCancel={() => setEditing(false)}
                onSaved={() => {
                  setEditing(false);
                  setSaved("Revised offer sent. The buyer has been notified.");
                  setReload((n) => n + 1);
                }}
              />
            ) : rfq.quote ? (
              <>
                <p style={{ fontSize: "1.4rem", fontWeight: 700, margin: "0 0 0.25rem" }}>
                  {money(rfq.quote.price_cents, rfq.quote.currency)}
                </p>
                <p className="muted small" style={{ marginTop: 0 }}>
                  {rfq.quote.delivery_days != null ? `Delivery in ${rfq.quote.delivery_days} day${rfq.quote.delivery_days === 1 ? "" : "s"}` : "No delivery time given"}
                  {rfq.quote.payment_terms_days != null ? ` · payment ${paymentTermsLabel(rfq.quote.payment_terms_days).toLowerCase()}` : ""}
                  {rfq.quote.version && rfq.quote.version > 1 ? ` · revision ${rfq.quote.version}` : ""}
                  {rfq.quote.updated_at ? ` · updated ${when(rfq.quote.updated_at)}` : ""}
                </p>
                <p style={{ margin: "0 0 0.5rem" }}>
                  Status: <strong style={{ textTransform: "capitalize" }}>{rfq.quote.status}</strong>
                </p>
                {rfq.quote.notes && <p className="muted" style={{ whiteSpace: "pre-wrap", marginBottom: 0 }}>{rfq.quote.notes}</p>}
                {live && rfq.quote.status === "submitted" && (
                  <button
                    type="button"
                    className="btn"
                    style={{ marginTop: "0.9rem" }}
                    onClick={() => {
                      setSaved(null);
                      setEditing(true);
                    }}
                  >
                    Revise quote
                  </button>
                )}
              </>
            ) : live ? (
              <>
                <SellerQuoteForm
                  token={token}
                  rfqId={rfq.id}
                  onSaved={() => {
                    setSaved("Quote sent. The buyer has been notified.");
                    setReload((n) => n + 1);
                  }}
                />
                <p className="muted small" style={{ marginBottom: 0 }}>
                  Prefer to quote automatically? <Link href="/seller/agent">Connect your sales agent</Link>.
                </p>
              </>
            ) : (
              <p className="muted" style={{ margin: 0 }}>This RFQ is no longer accepting quotes.</p>
            )}
          </div>

          {rfq.quote && (
            <div className="card">
              <h2 style={{ marginTop: 0 }}>Messages with the buyer</h2>
              <p className="muted small" style={{ marginTop: 0 }}>Clarifications and counter-offers on your quote.</p>
              <QuoteThread quoteId={rfq.quote.id} token={token} viewer="seller" />
            </div>
          )}
        </div>
      </div>
    </>
  );
}
