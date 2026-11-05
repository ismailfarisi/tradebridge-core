"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { PageHeader } from "@/components/AppShell";
import { OrderTimeline } from "@/components/OrderTimeline";
import { ReviewForm } from "@/components/ReviewForm";
import { DisputeModal } from "@/components/DisputeModal";
import { useMe } from "@/lib/auth";
import {
  createOrderDispute, createOrderReview, downloadInvoice, getOrder, listOrderDisputes,
  listOrderReviews, transitionOrder,
  orderRef, orderStatus,
  type Dispute, type Order, type OrderEvent, type OrderParties, type OrderStatus, type Review,
} from "@/lib/orders";
import { money, shortDate, when } from "@/lib/negotiation";

export default function OrderDetailPage() {
  const { token, me } = useMe();
  const params = useParams<{ id: string }>();
  const id = params.id;

  const [order, setOrder] = useState<Order | null>(null);
  const [events, setEvents] = useState<OrderEvent[]>([]);
  const [parties, setParties] = useState<OrderParties | null>(null);
  const [reviews, setReviews] = useState<Review[]>([]);
  const [disputes, setDisputes] = useState<Dispute[]>([]);
  const [notFound, setNotFound] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [showDispute, setShowDispute] = useState(false);

  const load = () => {
    if (!token || !id) return;
    getOrder(token, id)
      .then((r) => {
        setOrder(r.order);
        setEvents(r.events ?? []);
        setParties(r.parties ?? null);
      })
      .catch(() => setNotFound(true));
    listOrderReviews(token, id).then((r) => setReviews(r.reviews ?? [])).catch(() => {});
    listOrderDisputes(token, id).then((r) => setDisputes(r.disputes ?? [])).catch(() => {});
  };

  useEffect(() => {
    if (!token || !id) return;
    getOrder(token, id)
      .then((r) => {
        setOrder(r.order);
        setEvents(r.events ?? []);
        setParties(r.parties ?? null);
      })
      .catch(() => setNotFound(true));
    listOrderReviews(token, id).then((r) => setReviews(r.reviews ?? [])).catch(() => {});
    listOrderDisputes(token, id).then((r) => setDisputes(r.disputes ?? [])).catch(() => {});
  }, [token, id]);

  const transition = async (to: OrderStatus) => {
    if (!token || !id) return;
    setBusy(to);
    setErr(null);
    try {
      await transitionOrder(token, id, to);
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "transition failed");
    } finally {
      setBusy(null);
    }
  };

  const invoice = async () => {
    if (!token || !id) return;
    setBusy("invoice");
    setErr(null);
    try {
      const blob = await downloadInvoice(token, id);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `invoice-${orderRef(id).slice(1)}.pdf`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "invoice download failed");
    } finally {
      setBusy(null);
    }
  };

  if (notFound) return <p className="err">Order not found.</p>;
  if (err && !order) return <p className="err">{err}</p>;
  if (!order || !me) return <p className="muted">Loading…</p>;

  const role: "seller" | "buyer" = me.company_id === order.seller_company_id ? "seller" : "buyer";
  const hasOpenDispute = disputes.some((d) => d.status === "open");
  const myReview = reviews.find((r) => r.reviewer_company_id === me.company_id);

  const canConfirm = role === "seller" && order.status === "created";
  const canFulfil = role === "seller" && order.status === "confirmed";
  const canComplete = role === "buyer" && order.status === "fulfilled";
  const canCancel = order.status === "created" || order.status === "confirmed";
  const canDispute =
    !hasOpenDispute &&
    (order.status === "created" || order.status === "confirmed" || order.status === "fulfilled");

  return (
    <>
      <PageHeader
        title={parties?.rfq_title || `Order ${orderRef(order.id)}`}
        subtitle={`Order ${orderRef(order.id)} · placed ${when(order.created_at)}`}
        action={<StatusBadge status={order.status} />}
      />

      {order.status === "disputed" && (
        <p className="badge badge-warning" style={{ marginBottom: "1rem" }}>Under dispute — party actions are paused</p>
      )}

      <div className="card">
        <div className="row" style={{ justifyContent: "space-between", flexWrap: "wrap", gap: "0.5rem" }}>
          <div>
            <div className="num" style={{ fontSize: "1.25rem", fontWeight: 650 }}>{money(order.total_cents, order.currency)}</div>
            <div className="muted small">
              {role === "seller" ? "Buyer" : "Supplier"}:{" "}
              {(role === "seller" ? parties?.buyer_name : parties?.seller_name) || "—"}
            </div>
          </div>
          <div className="row" style={{ gap: "0.5rem", flexWrap: "wrap" }}>
            <button className="btn btn-sm" disabled={busy === "invoice"} onClick={invoice}>
              {busy === "invoice" ? "Downloading…" : "Download invoice (PDF)"}
            </button>
            {order.status !== "disputed" && canConfirm && (
              <button className="btn btn-sm btn-primary" disabled={!!busy} onClick={() => transition("confirmed")}>
                {busy === "confirmed" ? "…" : "Confirm order"}
              </button>
            )}
            {order.status !== "disputed" && canFulfil && (
              <button className="btn btn-sm btn-primary" disabled={!!busy} onClick={() => transition("fulfilled")}>
                {busy === "fulfilled" ? "…" : "Mark fulfilled"}
              </button>
            )}
            {order.status !== "disputed" && canComplete && (
              <button className="btn btn-sm btn-primary" disabled={!!busy} onClick={() => transition("completed")}>
                {busy === "completed" ? "…" : "Mark completed"}
              </button>
            )}
            {order.status !== "disputed" && canCancel && (
              <button className="btn btn-sm btn-ghost" disabled={!!busy} onClick={() => transition("cancelled")}>
                {busy === "cancelled" ? "…" : "Cancel order"}
              </button>
            )}
            {order.status !== "disputed" && canDispute && (
              <button className="btn btn-sm btn-ghost" disabled={!!busy} onClick={() => setShowDispute(true)}>
                Open a dispute
              </button>
            )}
          </div>
        </div>
        {err && <p className="err" style={{ marginTop: "0.6rem" }}>{err}</p>}
      </div>

      <div className="card" style={{ marginTop: "1rem" }}>
        <h2>Timeline</h2>
        <OrderTimeline events={events} />
      </div>

      <div className="card" style={{ marginTop: "1rem" }}>
        <h2>Reviews</h2>
        {reviews.length === 0 && <p className="muted">No reviews yet.</p>}
        {reviews.map((r) => (
          <div key={r.id} style={{ borderTop: "1px solid var(--border)", paddingTop: "0.5rem", marginTop: "0.5rem" }}>
            <div className="row small" style={{ gap: "0.4rem" }}>
              <strong>{"★".repeat(r.rating)}{"☆".repeat(5 - r.rating)}</strong>
              <span className="muted">{shortDate(r.created_at)}</span>
            </div>
            <p className="small" style={{ margin: "0.2rem 0 0" }}>{r.body}</p>
          </div>
        ))}
        {order.status === "completed" && !myReview && (
          <div style={{ marginTop: "1rem" }}>
            <ReviewForm onSubmit={async (rating, body) => { await createOrderReview(token!, id, rating, body); load(); }} />
          </div>
        )}
      </div>

      <div className="card" style={{ marginTop: "1rem" }}>
        <h2>Disputes</h2>
        {disputes.length === 0 && <p className="muted">No disputes.</p>}
        {disputes.map((d) => (
          <div key={d.id} style={{ borderTop: "1px solid var(--border)", paddingTop: "0.5rem", marginTop: "0.5rem" }}>
            <div className="row small" style={{ gap: "0.4rem", alignItems: "center" }}>
              <span className={`badge ${d.status === "open" ? "badge-warning" : "badge-success"}`}>{d.status}</span>
              <span className="muted">{d.reason.replace(/_/g, " ")} · {when(d.created_at)}</span>
            </div>
            <p className="small" style={{ margin: "0.2rem 0 0" }}>{d.detail}</p>
            {d.resolution && <p className="muted small" style={{ margin: "0.2rem 0 0" }}>Resolution: {d.resolution}</p>}
          </div>
        ))}
      </div>

      {showDispute && (
        <DisputeModal
          onClose={() => setShowDispute(false)}
          onSubmit={async (reason, detail) => { await createOrderDispute(token!, id, reason, detail); load(); }}
        />
      )}
    </>
  );
}

function StatusBadge({ status }: { status: OrderStatus }) {
  const st = orderStatus(status);
  return <span className={st.cls}>{st.label}</span>;
}
