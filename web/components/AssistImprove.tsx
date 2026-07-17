"use client";
import { useState } from "react";
import Link from "next/link";
import { assistRFQ } from "@/lib/assist";
import { PaywallError } from "@/lib/billing";

// "Improve this RFQ" — a small ghost button next to the composer's send
// control. Posts the current draft to /v1/assist/rfq and hands the returned
// suggestion back to the caller (which replaces the draft). A 402 becomes a
// small upgrade popover instead of a raw error; it never blocks sending.
export function AssistImprove({
  token,
  draft,
  onSuggestion,
  disabled,
}: {
  token: string;
  draft: string;
  onSuggestion: (suggestion: string) => void;
  disabled?: boolean;
}) {
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<{ kind: "paywall" | "error"; text: string; plan?: string } | null>(null);

  const improve = async () => {
    if (!draft.trim() || busy) return;
    setBusy(true);
    setNotice(null);
    try {
      const { suggestion } = await assistRFQ(token, draft);
      onSuggestion(suggestion);
    } catch (e) {
      if (e instanceof PaywallError) {
        setNotice({ kind: "paywall", text: "You've used this month's AI assist quota.", plan: e.planRequired });
      } else {
        setNotice({ kind: "error", text: e instanceof Error ? e.message : "Couldn't improve draft — retry" });
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={{ position: "relative" }}>
      <button
        type="button"
        className="btn btn-sm btn-ghost"
        disabled={disabled || busy || !draft.trim()}
        onClick={improve}
        title="Improve this RFQ draft with AI"
      >
        {busy ? "…" : "✨ Improve"}
      </button>
      {notice && (
        <div
          className="card"
          style={{ position: "absolute", bottom: "calc(100% + 0.4rem)", right: 0, width: 220, padding: "0.6rem 0.75rem", zIndex: 10 }}
        >
          <p className={notice.kind === "error" ? "err small" : "muted small"} style={{ margin: 0 }}>
            {notice.text}{" "}
            {notice.kind === "paywall" && <Link href="/settings/billing">Upgrade →</Link>}
          </p>
        </div>
      )}
    </div>
  );
}
