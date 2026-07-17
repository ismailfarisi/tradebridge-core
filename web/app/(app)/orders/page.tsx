"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { PageHeader } from "@/components/AppShell";
import { useMe } from "@/lib/auth";
import { listOrders, type Order } from "@/lib/orders";

export default function OrdersPage() {
  const { token, me } = useMe();
  const [orders, setOrders] = useState<Order[]>([]);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!token) return;
    listOrders(token).then((r) => setOrders(r.orders)).catch((e) => setErr(e.message));
  }, [token]);

  return (
    <>
      <PageHeader title="Orders" subtitle="Purchase orders you're a party to, as buyer or seller." />
      {err && <p className="err">{err}</p>}
      <div className="card">
        <table className="tbl">
          <thead>
            <tr>
              <th>Order</th>
              <th>Counterparty</th>
              <th>Total</th>
              <th>Status</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {orders.map((o) => {
              const isSeller = me?.company_id === o.seller_company_id;
              const counterparty = isSeller ? o.buyer_company_id : o.seller_company_id;
              return (
                <tr key={o.id}>
                  <td><Link href={`/orders/${o.id}`}>{o.id.slice(0, 8)}</Link></td>
                  <td className="muted">{isSeller ? "Buyer" : "Seller"} {counterparty.slice(0, 8)}</td>
                  <td><strong>{o.currency} {(o.total_cents / 100).toFixed(2)}</strong></td>
                  <td><span className="badge">{o.status}</span></td>
                  <td className="muted small">{new Date(o.created_at).toLocaleDateString()}</td>
                </tr>
              );
            })}
            {orders.length === 0 && (
              <tr><td colSpan={5} className="muted">No orders yet.</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}
