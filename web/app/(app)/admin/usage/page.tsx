"use client";

import { PageHeader } from "@/components/AppShell";

export default function UsagePage() {
  return (
    <>
      <PageHeader title="Usage & billing" subtitle="Requests, tokens and spend across the platform." />

      <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))", marginBottom: "1.5rem" }}>
        <Metric n="—" label="RFQs (30d)" />
        <Metric n="—" label="Quotes generated" />
        <Metric n="—" label="LLM tokens" />
        <Metric n="—" label="Est. spend" />
      </div>

      <div className="card">
        <h2>Usage by day</h2>
        <div className="placeholder" style={{ marginTop: "0.75rem" }}>
          Metering not wired yet — this will chart requests, token usage per provider/model, and
          estimated cost once usage events are recorded.
        </div>
      </div>

      <p className="notice" style={{ marginTop: "1rem" }}>
        <strong>Skeleton.</strong> Planned: per-company &amp; per-model token counters via a
        usage-events table, cost estimates from provider pricing, exportable invoices, and quota alerts.
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
