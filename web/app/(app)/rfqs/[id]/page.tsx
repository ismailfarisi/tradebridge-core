"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { PageHeader } from "@/components/AppShell";
import { StatusBadge } from "../page";
import { VerifiedBadge } from "@/components/VerifiedBadge";
import { StarRating } from "@/components/StarRating";
import { useToken } from "@/lib/auth";
import { acceptQuote, getRfq, rfqQuotes, type QuoteRow, type RFQ } from "@/lib/api";

export default function RfqDetail() {
  const token = useToken();
  const params = useParams<{ id: string }>();
  const id = params.id;

  const [rfq, setRfq] = useState<RFQ | null>(null);
  const [quotes, setQuotes] = useState<QuoteRow[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const [acceptedOrderId, setAcceptedOrderId] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const load = () => {
    if (!token || !id) return;
    getRfq(token, id).then((r) => setRfq(r.rfq)).catch((e) => setErr(e.message));
    rfqQuotes(token, id).then((r) => setQuotes(r.quotes)).catch(() => {});
  };

  useEffect(() => {
    if (!token || !id) return;
    getRfq(token, id).then((r) => setRfq(r.rfq)).catch((e) => setErr(e.message));
    rfqQuotes(token, id).then((r) => setQuotes(r.quotes)).catch(() => {});
  }, [token, id]);

  const accept = async (quoteId: string) => {
    if (!token) return;
    setBusy(quoteId);
    setMsg(null);
    try {
      const { order_id, total_cents } = await acceptQuote(token, quoteId);
      setMsg(`Accepted — purchase order created for $${(total_cents / 100).toFixed(2)}.`);
      setAcceptedOrderId(order_id);
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "accept failed");
    } finally {
      setBusy(null);
    }
  };

  if (err) return <p className="err">{err}</p>;
  if (!rfq) return <p className="muted">Loading…</p>;

  const req = rfq.requirements as Record<string, unknown>;

  return (
    <>
      <PageHeader
        title={rfq.title}
        subtitle={`${rfq.category || "uncategorized"} · ${rfq.match_count} matched`}
        action={<StatusBadge status={rfq.status} />}
      />
      <div style={{ marginBottom: "-0.75rem" }}><Link href="/rfqs" className="small">← All RFQs</Link></div>

      <div className="card" style={{ margin: "1rem 0 1.5rem" }}>
        <h2>Extracted requirements</h2>
        <div className="row wrap" style={{ marginTop: "0.5rem", gap: "0.4rem" }}>
          {Object.entries(req)
            .filter(([, v]) => v && (!Array.isArray(v) || v.length))
            .map(([k, v]) => (
              <span key={k} className="badge">{k}: {Array.isArray(v) ? v.join(", ") : String(v)}</span>
            ))}
        </div>
        <p className="muted small" style={{ marginTop: "0.75rem" }}>{rfq.raw_text}</p>
      </div>

      {msg && (
        <p className="ok" style={{ marginBottom: "1rem" }}>
          {msg}{" "}
          {acceptedOrderId && <Link href={`/orders/${acceptedOrderId}`}>View order →</Link>}
        </p>
      )}

      <div className="card">
        <h2>Quotes</h2>
        <table className="tbl" style={{ marginTop: "0.5rem" }}>
          <thead>
            <tr>
              <th>#</th>
              <th>Seller</th>
              <th>Price</th>
              <th>Delivery</th>
              <th>Details</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {quotes.map((q, i) => (
              <tr key={q.id}>
                <td>{q.rank ?? i + 1}</td>
                <td>
                  {q.seller_name ?? q.seller_company_id.slice(0, 8)}{" "}
                  <VerifiedBadge status={q.seller_verification_status} />{" "}
                  {q.seller_review_count ? (
                    <StarRating value={q.seller_avg_rating ?? 0} count={q.seller_review_count} />
                  ) : (
                    <span className="muted">No ratings</span>
                  )}
                </td>
                <td><strong>{q.currency} {(q.price_cents / 100).toFixed(2)}</strong></td>
                <td className="muted">{q.delivery_days != null ? `${q.delivery_days} days` : "—"}</td>
                <td className="muted small">{q.notes}</td>
                <td style={{ textAlign: "right" }}>
                  {q.status === "accepted" ? (
                    <span className="badge badge-success">accepted</span>
                  ) : q.status === "rejected" ? (
                    <span className="badge">rejected</span>
                  ) : rfq.status !== "closed" ? (
                    <button className="btn btn-sm btn-primary" disabled={busy === q.id} onClick={() => accept(q.id)}>
                      {busy === q.id ? "…" : "Accept"}
                    </button>
                  ) : null}
                </td>
              </tr>
            ))}
            {quotes.length === 0 && (
              <tr><td colSpan={6} className="muted">No quotes yet — sellers&apos; agents respond automatically.</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}
