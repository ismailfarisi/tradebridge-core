"use client";

import { useEffect, useState } from "react";
import AdminShell from "@/components/AdminShell";
import { useAdminToken } from "@/lib/admin";
import { listFeedback, type FeedbackRow } from "@/lib/feedbackAdmin";
import { npsScore } from "@/lib/feedback";

export default function AdminFeedbackPage() {
  return (
    <AdminShell>
      <FeedbackConsole />
    </AdminShell>
  );
}

function FeedbackConsole() {
  const token = useAdminToken();
  const [rows, setRows] = useState<FeedbackRow[]>([]);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!token) return;
    listFeedback(token)
      .then((r) => setRows(r.feedback))
      .catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  }, [token]);

  const npsRows = rows.filter((r) => r.kind === "nps" && r.score != null);
  const nps = npsScore(npsRows.map((r) => r.score as number));

  return (
    <>
      <div style={{ marginBottom: "1.25rem" }}>
        <h1 style={{ fontSize: "1.4rem" }}>Feedback</h1>
        <p className="muted" style={{ margin: "0.25rem 0 0" }}>
          In-app feedback and NPS ratings submitted by tenants, newest first.
        </p>
      </div>

      {err && <p className="err">{err}</p>}

      <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))", marginBottom: "1.5rem" }}>
        <Stat n={npsRows.length > 0 ? nps : "—"} label={`NPS (${npsRows.length} responses)`} />
        <Stat n={rows.filter((r) => r.kind === "feedback").length} label="Free-text notes" />
        <Stat n={rows.length} label="Total entries" />
      </div>

      <div className="card">
        <table className="tbl">
          <thead>
            <tr>
              <th>When</th>
              <th>Kind</th>
              <th>Score</th>
              <th>Message</th>
              <th>Company</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.id}>
                <td className="muted small">{new Date(r.created_at).toLocaleString()}</td>
                <td>
                  <span className={r.kind === "nps" ? "badge badge-info" : "badge"}>{r.kind}</span>
                </td>
                <td>{r.score ?? "—"}</td>
                <td>{r.message ?? "—"}</td>
                <td className="muted small">{r.company_id}</td>
              </tr>
            ))}
            {rows.length === 0 && !err && (
              <tr><td colSpan={5} className="muted">No feedback yet.</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}

function Stat({ n, label }: { n: React.ReactNode; label: string }) {
  return (
    <div className="card" style={{ padding: "0.9rem 1rem" }}>
      <div style={{ fontSize: "1.5rem", fontWeight: 700 }}>{n}</div>
      <div className="muted small">{label}</div>
    </div>
  );
}
