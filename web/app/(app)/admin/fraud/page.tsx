"use client";

import { PageHeader } from "@/components/AppShell";

const SIGNALS = [
  { name: "Velocity", desc: "Abnormal RFQ / quote volume from one company", state: "idle" },
  { name: "Price anomalies", desc: "Quotes far from catalog or market norms", state: "idle" },
  { name: "Duplicate identities", desc: "Repeated emails / domains across companies", state: "idle" },
  { name: "Credential abuse", desc: "Agent credentials used from many sources", state: "idle" },
];

export default function FraudPage() {
  return (
    <>
      <PageHeader title="Fraud detection" subtitle="Risk signals and flagged activity across the marketplace." />

      <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))", marginBottom: "1.5rem" }}>
        <Metric n="0" label="Open flags" />
        <Metric n="0" label="Companies at risk" />
        <Metric n="—" label="Signals enabled" />
      </div>

      <div className="card" style={{ marginBottom: "1.5rem" }}>
        <h2>Detection signals</h2>
        <table className="tbl" style={{ marginTop: "0.5rem" }}>
          <thead><tr><th>Signal</th><th>What it catches</th><th>Status</th></tr></thead>
          <tbody>
            {SIGNALS.map((s) => (
              <tr key={s.name}>
                <td><strong>{s.name}</strong></td>
                <td className="muted small">{s.desc}</td>
                <td><span className="badge badge-warning">not configured</span></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="card">
        <h2>Flagged activity</h2>
        <div className="placeholder" style={{ marginTop: "0.75rem" }}>
          No flags. When signals are enabled, suspicious RFQs, quotes and accounts appear here for review
          (allow / suspend / escalate).
        </div>
      </div>

      <p className="notice" style={{ marginTop: "1rem" }}>
        <strong>Skeleton.</strong> Planned: a rules engine over the audit log + order/quote history,
        a review queue with actions, and webhook alerts.
      </p>
    </>
  );
}

function Metric({ n, label }: { n: string; label: string }) {
  return (
    <div className="stat">
      <div className="n">{n}</div>
      <div className="l">{label}</div>
    </div>
  );
}
