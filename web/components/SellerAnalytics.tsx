"use client";

import { useEffect, useState } from "react";
import { getSellerAnalytics, type SellerAnalytics } from "@/lib/analytics";

export function SellerAnalyticsPanel({ token }: { token: string }) {
  const [a, setA] = useState<SellerAnalytics | null>(null);
  useEffect(() => { getSellerAnalytics(token).then(setA).catch(() => {}); }, [token]);
  if (!a) return null;
  const money = (c: number) => `$${(c / 100).toLocaleString(undefined, { minimumFractionDigits: 2 })}`;
  return (
    <div className="card" style={{ marginTop: "1.5rem" }}>
      <h2 style={{ marginTop: 0 }}>Performance</h2>
      <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))" }}>
        <Stat n={`${Math.round(a.win_rate * 100)}%`} label={`Win rate (${a.quotes_won}/${a.quotes_submitted})`} />
        <Stat n={money(a.revenue_won_cents)} label="Revenue won" />
        <Stat n={a.avg_response_hours ? `${a.avg_response_hours.toFixed(1)}h` : "—"} label="Avg response" />
      </div>
      {a.top_buyers.length > 0 && (
        <>
          <h3>Top buyers</h3>
          <table className="tbl"><tbody>
            {a.top_buyers.map((b) => (
              <tr key={b.company_id}><td>{b.name}</td><td>{b.orders} order{b.orders === 1 ? "" : "s"}</td><td>{money(b.revenue_cents)}</td></tr>
            ))}
          </tbody></table>
        </>
      )}
    </div>
  );
}

function Stat({ n, label }: { n: string | number; label: string }) {
  return <div className="stat"><div className="n">{n}</div><div className="l">{label}</div></div>;
}
