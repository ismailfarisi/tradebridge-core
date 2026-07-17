"use client";

import { useEffect, useState } from "react";
import { PageHeader } from "@/components/AppShell";
import { useToken } from "@/lib/auth";
import { createRfq, listRfqs, type RFQ } from "@/lib/api";
import { EmptyState } from "@/components/EmptyState";
import { UpgradeNudge } from "@/components/UpgradeNudge";
import { getBillingSummary, type BillingSummary } from "@/lib/billing";

export default function RfqsPage() {
  const token = useToken();
  const [rfqs, setRfqs] = useState<RFQ[]>([]);
  const [billing, setBilling] = useState<BillingSummary | null>(null);
  const [title, setTitle] = useState("");
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [tab, setTab] = useState("");
  const [search, setSearch] = useState("");

  const refresh = () => {
    if (!token) return;
    listRfqs(token, { tab, q: search }).then((r) => setRfqs(r.rfqs)).catch((e) => setErr(e.message));
  };

  useEffect(() => {
    if (!token) return;
    listRfqs(token, { tab, q: search }).then((r) => setRfqs(r.rfqs)).catch((e) => setErr(e.message));
  }, [token, tab, search]);

  useEffect(() => {
    if (!token) return;
    getBillingSummary(token).then(setBilling).catch(() => {});
  }, [token]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true);
    setMsg(null);
    setErr(null);
    try {
      const { match_count } = await createRfq(token, title, text);
      setMsg(`RFQ created — matched ${match_count} seller${match_count === 1 ? "" : "s"}.`);
      setTitle("");
      setText("");
      refresh();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <PageHeader title="RFQs" subtitle="Post a request; Core extracts requirements and matches sellers." />

      <div className="card" style={{ marginBottom: "1.5rem" }}>
        <h2>New request</h2>
        <form className="col" onSubmit={submit} style={{ marginTop: "0.75rem" }}>
          <div className="field">
            <label>Title</label>
            <input className="input" value={title} onChange={(e) => setTitle(e.target.value)} placeholder="e.g. Cartons for packaging" required />
          </div>
          <div className="field">
            <label>Describe what you need</label>
            <textarea className="input" rows={3} value={text} onChange={(e) => setText(e.target.value)} placeholder="Need 500 cartons for packaging, deliver to Dubai by next month." required />
          </div>
          {msg && <p className="ok">{msg}</p>}
          {err && <p className="err">{err}</p>}
          <div>
            <button className="btn btn-primary" disabled={busy}>{busy ? "Posting…" : "Post RFQ"}</button>
          </div>
        </form>
      </div>

      {billing && (
        <UpgradeNudge feature="rfq_monthly_limit" used={billing.usage.rfq_used} limit={billing.usage.rfq_limit} />
      )}

      <div className="card">
        <h2>Your RFQs</h2>
        <div className="row" style={{ gap: "0.5rem", margin: "1rem 0", alignItems: "center", flexWrap: "wrap" }}>
          {[["", "All"], ["open", "Open"], ["quoted", "Quoted"], ["closed", "Closed"]].map(([val, label]) => (
            <button key={val} className="btn" aria-pressed={tab === val} onClick={() => setTab(val)}>{label}</button>
          ))}
          <input className="input" placeholder="Search RFQs…" value={search} onChange={(e) => setSearch(e.target.value)} style={{ flex: 1, minWidth: 180 }} />
        </div>
        <table className="tbl" style={{ marginTop: "0.5rem" }}>
          <thead>
            <tr>
              <th>Title</th>
              <th>Category</th>
              <th>Status</th>
              <th>Matches</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {rfqs.map((r) => (
              <tr key={r.id}>
                <td>
                  {r.title}{" "}
                  {r.new_quotes && r.new_quotes > 0 ? <span className="badge">{r.new_quotes} new</span> : null}
                  {typeof r.quote_count === "number" && <span className="muted small"> {r.quote_count} quote{r.quote_count === 1 ? "" : "s"}</span>}
                </td>
                <td className="muted">{r.category || "—"}</td>
                <td><StatusBadge status={r.status} /></td>
                <td>{r.match_count}</td>
                <td style={{ textAlign: "right" }}><a href={`/rfqs/${r.id}`}>View →</a></td>
              </tr>
            ))}
            {rfqs.length === 0 && (
              <tr><td colSpan={5}>
                <EmptyState
                  title={tab || search ? "No RFQs match this filter" : "No RFQs yet"}
                  body={tab || search ? "Try a different tab or clear your search." : "Post your first request and Core will match sellers and collect quotes."}
                  cta={tab || search ? { label: "Clear filters", onClick: () => { setTab(""); setSearch(""); } } : { label: "Post an RFQ", href: "/chat" }}
                />
              </td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}

export function StatusBadge({ status }: { status: string }) {
  const cls =
    status === "closed" ? "badge" :
    status === "quoted" ? "badge badge-success" :
    status === "open" ? "badge badge-info" : "badge badge-warning";
  return <span className={cls}>{status}</span>;
}
