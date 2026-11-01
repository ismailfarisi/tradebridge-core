"use client";

import { useState } from "react";
import { Icon } from "@/components/Icon";
import {
  canManagePause,
  pauseDetail,
  pauseHeadline,
  setCompanyPause,
  type PauseInfo,
} from "@/lib/negotiationControls";

// NegotiationPauseBanner tells the buyer that automated negotiation is
// stopped and by whom.
export function NegotiationPauseBanner({ pause }: { pause: PauseInfo | null | undefined }) {
  if (!pause?.paused) return null;
  return (
    <div className="banner banner-warn" role="status">
      <Icon name="pause" />
      <div className="grow">
        <strong>{pauseHeadline(pause)}.</strong> <span className="small">{pauseDetail(pause)}</span>
      </div>
    </div>
  );
}

// NegotiationPauseControl is the company-wide kill switch on the
// negotiations page: the banner for everyone, plus pause/resume for owners
// and admins.
export function NegotiationPauseControl({
  token,
  role,
  pause,
  onChange,
}: {
  token: string;
  role: string | null | undefined;
  pause: PauseInfo;
  onChange: (p: PauseInfo) => void;
}) {
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const manage = canManagePause(role);

  const flip = async (paused: boolean) => {
    if (paused && !window.confirm("Pause every automated negotiation for your company?")) return;
    setBusy(true);
    setErr(null);
    try {
      onChange(await setCompanyPause(token, paused, reason.trim()));
      setReason("");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Could not update");
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <NegotiationPauseBanner pause={pause} />
      {manage && (
        <div className="row" style={{ gap: "0.5rem", marginBottom: "1rem", flexWrap: "wrap", alignItems: "center" }}>
          {pause.company_paused ? (
            <button className="btn btn-sm btn-primary" disabled={busy} onClick={() => flip(false)}>
              <Icon name="play" size={14} /> Resume company negotiations
            </button>
          ) : (
            <>
              <input
                className="input"
                style={{ maxWidth: 320 }}
                placeholder="Reason (optional)"
                value={reason}
                maxLength={500}
                onChange={(e) => setReason(e.target.value)}
                aria-label="Reason for pausing"
              />
              <button className="btn btn-sm" disabled={busy} onClick={() => flip(true)}>
                <Icon name="pause" size={14} /> Pause all negotiations
              </button>
            </>
          )}
          {pause.platform_paused && pause.company_paused && (
            <span className="small muted">The platform pause stays in effect even after you resume.</span>
          )}
          {err && <span className="err small">{err}</span>}
        </div>
      )}
    </>
  );
}
