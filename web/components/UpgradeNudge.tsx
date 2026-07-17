"use client";

import { useState } from "react";
import UpgradeModal from "@/components/UpgradeModal";
import { usageLabel } from "@/lib/billing";
import { shouldNudge } from "@/lib/activation";

// UpgradeNudge shows a near-limit banner when usage approaches the plan limit,
// or a locked-feature CTA when `locked`. Both open the shared UpgradeModal.
export function UpgradeNudge({
  feature,
  used,
  limit,
  planRequired = "pro",
  locked = false,
}: {
  feature: string;
  used?: number;
  limit?: number;
  planRequired?: string;
  locked?: boolean;
}) {
  const [open, setOpen] = useState(false);

  const near = !locked && typeof used === "number" && typeof limit === "number" && shouldNudge(used, limit);
  if (!locked && !near) return null;

  return (
    <>
      <div className="card" style={{ borderLeft: "3px solid var(--accent, #2563eb)", marginBottom: "1rem" }}>
        <div className="row" style={{ justifyContent: "space-between", alignItems: "center", gap: "0.75rem", flexWrap: "wrap" }}>
          <span className="small">
            {locked
              ? "This feature is on a higher plan."
              : `You're near your plan limit${typeof used === "number" && typeof limit === "number" ? ` (${usageLabel(used, limit)})` : ""}.`}
          </span>
          <button className="btn" onClick={() => setOpen(true)}>Upgrade</button>
        </div>
      </div>
      {open && (
        <UpgradeModal
          feature={feature}
          planRequired={planRequired}
          onClose={() => setOpen(false)}
          onUpgrade={() => { window.location.href = "/settings/billing"; }}
        />
      )}
    </>
  );
}
