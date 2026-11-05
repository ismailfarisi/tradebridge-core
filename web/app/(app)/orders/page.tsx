"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { PageHeader } from "@/components/AppShell";
import { useMe } from "@/lib/auth";
import { listOrders, orderRef, orderStatus, type Order } from "@/lib/orders";
import { money, shortDate } from "@/lib/negotiation";
import { EmptyState } from "@/components/EmptyState";

export default function OrdersPage() {
  const { token, me } = useMe();
  const [orders, setOrders] = useState<Order[]>([]);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!token) return;
    listOrders(token).then((r) => setOrders(r.orders ?? [])).catch(() => setErr("Couldn't load your orders. Refresh to try again."));
  }, [token]);

  const sellsToo = me?.company_type === "both";

  return (
    <>
      <PageHeader title="Orders" subtitle="Purchase orders from accepted quotes." />
      {err && <p className="err">{err}</p>}
      <div className="card" style={{ padding: 0 }}>
        <table className="tbl tbl-cards">
          <thead>
            <tr>
              <th>Order</th>
              <th>{sellsToo ? "With" : me?.company_type === "seller" ? "Buyer" : "Supplier"}</th>
              <th style={{ textAlign: "right" }}>Total</th>
              <th>Status</th>
              <th>Date</th>
            </tr>
          </thead>
          <tbody>
            {orders.map((o) => {
              const isSeller = me?.company_id === o.seller_company_id;
              const other = isSeller ? o.buyer_name : o.seller_name;
              const st = orderStatus(o.status);
              return (
                <tr key={o.id}>
                  <td className="tc-main">
                    <Link className="rowlink" href={`/orders/${o.id}`}>{o.rfq_title || `Order ${orderRef(o.id)}`}</Link>
                    <div className="muted small num">{orderRef(o.id)}</div>
                  </td>
                  <td className="tc-main tc-sub">
                    {sellsToo && <span className="muted">{isSeller ? "Buyer · " : "Supplier · "}</span>}
                    {other || "—"}
                  </td>
                  <td className="num" data-label="Total" style={{ textAlign: "right", fontWeight: 600 }}>{money(o.total_cents, o.currency)}</td>
                  <td data-label="Status"><span className={st.cls}>{st.label}</span></td>
                  <td className="muted tc-hide-m" style={{ whiteSpace: "nowrap" }}>{shortDate(o.created_at)}</td>
                </tr>
              );
            })}
            {orders.length === 0 && !err && (
              <tr><td colSpan={5} className="tc-main">
                <EmptyState
                  title="No orders yet"
                  body="Accepted quotes become purchase orders and show up here."
                  cta={{ label: "Go to dashboard", href: "/dashboard" }}
                />
              </td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}
