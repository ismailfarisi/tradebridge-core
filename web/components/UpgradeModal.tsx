"use client";

// Copy for each gated feature — names the specific benefit the plan unlocks.
const FEATURE_COPY: Record<string, string> = {
  agent_autonomy: "Let your agent send quotes 24/7 without manual review.",
  rfq_monthly_limit: "Post more RFQs each month.",
  catalog_max_items: "List more products in your catalog.",
  seats: "Invite more teammates.",
};

export default function UpgradeModal({
  feature,
  planRequired,
  onClose,
  onUpgrade,
  busy,
}: {
  feature: string;
  planRequired: string;
  onClose: () => void;
  onUpgrade?: () => void;
  busy?: boolean;
}) {
  const plan = planRequired ? planRequired[0].toUpperCase() + planRequired.slice(1) : "Pro";
  const benefit = FEATURE_COPY[feature] ?? "Unlock more of TradeBridge.";
  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      style={{
        position: "fixed", inset: 0, background: "rgba(0,0,0,0.6)",
        display: "flex", alignItems: "center", justifyContent: "center", padding: "1rem", zIndex: 50,
      }}
    >
      <div className="card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: 420, width: "100%" }}>
        <span className="badge badge-info" style={{ marginBottom: "0.75rem" }}>{plan} plan</span>
        <h2 style={{ fontSize: "1.2rem", marginBottom: "0.5rem" }}>Upgrade to unlock this</h2>
        <p className="muted" style={{ marginBottom: "1.25rem" }}>{benefit}</p>
        <div className="row" style={{ gap: "0.6rem", justifyContent: "flex-end" }}>
          <button className="btn btn-ghost" onClick={onClose} disabled={busy}>Not now</button>
          {onUpgrade && (
            <button className="btn btn-primary" onClick={onUpgrade} disabled={busy}>
              {busy ? "Redirecting…" : `Upgrade to ${plan}`}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
