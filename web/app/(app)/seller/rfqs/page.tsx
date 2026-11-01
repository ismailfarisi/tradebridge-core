"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { PageHeader } from "@/components/AppShell";
import { EmptyState } from "@/components/EmptyState";
import { useToken } from "@/lib/auth";
import { money } from "@/lib/negotiation";
import { daysLeft, listSellerRfqs, sellerState, type SellerRFQ, type SellerTab } from "@/lib/seller";

const TABS: [SellerTab, string][] = [
  ["open", "Awaiting quote"],
  ["quoted", "Quoted"],
  ["closed", "Closed"],
  ["", "All"],
];

// tabFromQuery maps a ?tab= value to a tab; "all" is the unfiltered list.
function tabFromQuery(t: string | null): SellerTab | null {
  if (t === "all") return "";
  if (t === "open" || t === "quoted" || t === "closed") return t;
  return null;
}

export default function SellerRfqsPage() {
  const token = useToken();
  const [rfqs, setRfqs] = useState<SellerRFQ[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [tab, setTab] = useState<SellerTab>("open");
  const [search, setSearch] = useState("");

  // Deep links such as the dashboard's "Incoming RFQs" pick the starting tab
  // with ?tab=all|open|quoted|closed (read on the client, like /register).
  useEffect(() => {
    const t = new URLSearchParams(window.location.search).get("tab");
    const initial = tabFromQuery(t);
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (initial !== null) setTab(initial);
  }, []);

  useEffect(() => {
    if (!token) return;
    let alive = true;
    listSellerRfqs(token, { tab, q: search })
      .then((r) => {
        if (!alive) return;
        setRfqs(r.rfqs);
        setErr(null);
      })
      .catch((e) => alive && setErr(e instanceof Error ? e.message : "failed to load RFQs"));
    return () => {
      alive = false;
    };
  }, [token, tab, search]);

  const filtered = Boolean(search) || tab !== "open";

  return (
    <>
      <PageHeader
        title="RFQ inbox"
        subtitle="Buyer requests matched to your supplier profile and catalog."
      />

      <div className="card">
        <div className="row" style={{ gap: "0.5rem", marginBottom: "1rem", alignItems: "center", flexWrap: "wrap" }}>
          {TABS.map(([val, label]) => (
            <button key={val || "all"} className="btn" aria-pressed={tab === val} onClick={() => setTab(val)}>
              {label}
            </button>
          ))}
          <input
            className="input"
            placeholder="Search RFQs…"
            aria-label="Search RFQs"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            style={{ flex: 1, minWidth: 180 }}
          />
        </div>

        {err && <p className="err">{err}</p>}
        {!rfqs && !err && <p className="muted">Loading…</p>}

        {rfqs && rfqs.length > 0 && (
          <div style={{ overflowX: "auto" }}>
            <table className="tbl">
              <thead>
                <tr>
                  <th>Request</th>
                  <th>Quantity</th>
                  <th>Deliver to</th>
                  <th>Status</th>
                  <th>Your quote</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {rfqs.map((r) => {
                  const state = sellerState(r);
                  const left = r.status === "open" || r.status === "quoted" ? daysLeft(r.expires_at) : null;
                  return (
                    <tr key={r.id}>
                      <td>
                        <Link className="rowlink" href={`/seller/rfqs/${r.id}`}>{r.title}</Link>{" "}
                        {r.unseen && <span className="badge badge-info">New</span>}
                        <div className="muted small">
                          {r.category || "Uncategorised"}
                          {left !== null && ` · ${left} day${left === 1 ? "" : "s"} left`}
                        </div>
                      </td>
                      <td>{r.requirements.quantity || <span className="muted">—</span>}</td>
                      <td>{r.requirements.location || <span className="muted">—</span>}</td>
                      <td><span className={state.cls}>{state.label}</span></td>
                      <td>{r.quote ? money(r.quote.price_cents, r.quote.currency) : <span className="muted">—</span>}</td>
                      <td style={{ textAlign: "right" }}>
                        <Link href={`/seller/rfqs/${r.id}`}>View →</Link>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {rfqs && rfqs.length === 0 && (
          <EmptyState
            title={filtered ? "No RFQs here" : "No RFQs waiting for a quote"}
            body={
              filtered
                ? "Try another tab or clear your search."
                : "Buyers' requests appear here when they match your supplier profile. Add the categories you supply so Core can match you."
            }
            cta={
              filtered
                ? { label: "Show RFQs awaiting a quote", onClick: () => { setTab("open"); setSearch(""); } }
                : { label: "Update supplier profile", href: "/seller/profile" }
            }
          />
        )}
      </div>
    </>
  );
}
