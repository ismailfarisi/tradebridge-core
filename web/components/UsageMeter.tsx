"use client";

import { usageLabel, usagePercent } from "@/lib/billing";

/** A labelled usage bar. Unlimited (-1) renders a full-width "∞" with no fill. */
export default function UsageMeter({ label, used, limit }: { label: string; used: number; limit: number }) {
  const pct = usagePercent(used, limit);
  const near = limit >= 0 && pct >= 80;
  const unlimited = limit < 0;
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "0.35rem" }}>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <span style={{ fontWeight: 550 }}>{label}</span>
        <span className={near ? "" : "muted"} style={near ? { color: "var(--warning)" } : undefined}>
          {usageLabel(used, limit)}
        </span>
      </div>
      <div style={{ height: 8, borderRadius: 6, background: "var(--surface-2)", overflow: "hidden" }}>
        {!unlimited && (
          <div
            style={{
              height: "100%",
              width: `${pct}%`,
              background: near ? "var(--warning)" : "var(--primary)",
              transition: "width 200ms ease",
            }}
          />
        )}
      </div>
    </div>
  );
}
