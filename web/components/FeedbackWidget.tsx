"use client";

import { useEffect, useState } from "react";
import { useToken } from "@/lib/auth";
import { sendFeedback } from "@/lib/feedback";

type Tab = "feedback" | "nps";

const OPEN_EVENT = "tb:feedback";

// openFeedback lets other shell controls (account menu, phone "More" sheet) open the dialog.
export const openFeedback = () => window.dispatchEvent(new Event(OPEN_EVENT));

// FeedbackWidget is a small fixed-position launcher mounted authed-only in
// AppShell. It opens a modal with two tabs — free-text feedback and a 0-10
// NPS rating — and posts straight to POST /v1/feedback via sendFeedback.
export default function FeedbackWidget() {
  const token = useToken();
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<Tab>("feedback");
  const [message, setMessage] = useState("");
  const [score, setScore] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [toast, setToast] = useState(false);

  useEffect(() => {
    const onOpen = () => setOpen(true);
    window.addEventListener(OPEN_EVENT, onOpen);
    return () => window.removeEventListener(OPEN_EVENT, onOpen);
  }, []);

  if (!token) return null;

  const close = () => {
    setOpen(false);
    setErr(null);
  };

  const submit = async () => {
    setErr(null);
    setBusy(true);
    try {
      if (tab === "feedback") {
        if (!message.trim()) {
          setErr("Say a bit more before sending.");
          setBusy(false);
          return;
        }
        await sendFeedback(token, { kind: "feedback", message: message.trim() });
        setMessage("");
      } else {
        if (score === null) {
          setErr("Pick a score from 0 to 10.");
          setBusy(false);
          return;
        }
        await sendFeedback(token, { kind: "nps", score });
        setScore(null);
      }
      close();
      setToast(true);
      setTimeout(() => setToast(false), 2500);
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "failed to send feedback");
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <button
        type="button"
        className="btn btn-sm btn-primary fb-launch"
        onClick={() => setOpen(true)}
        style={{ position: "fixed", right: "1.25rem", bottom: "1.25rem", zIndex: 40, borderRadius: 999 }}
      >
        Feedback
      </button>

      {toast && (
        <div
          role="status"
          aria-live="polite"
          style={{
            position: "fixed", right: "1.25rem", bottom: "4.25rem", zIndex: 60,
            background: "var(--surface-2, #222)", border: "1px solid var(--border)",
            borderRadius: 8, padding: "0.55rem 0.9rem", fontSize: "0.85rem", fontWeight: 600,
          }}
        >
          Thanks for the feedback!
        </div>
      )}

      {open && (
        <div
          role="dialog"
          aria-modal="true"
          onClick={close}
          style={{
            position: "fixed", inset: 0, background: "rgba(0,0,0,0.6)",
            display: "flex", alignItems: "center", justifyContent: "center", padding: "1rem", zIndex: 50,
          }}
        >
          <div className="card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: 420, width: "100%" }}>
            <h2 style={{ fontSize: "1.15rem", marginBottom: "0.75rem" }}>Feedback</h2>

            <div className="row" style={{ gap: "0.4rem", marginBottom: "0.75rem" }}>
              <button
                type="button"
                className={tab === "feedback" ? "btn btn-sm btn-primary" : "btn btn-sm btn-ghost"}
                onClick={() => { setTab("feedback"); setErr(null); }}
              >
                Send feedback
              </button>
              <button
                type="button"
                className={tab === "nps" ? "btn btn-sm btn-primary" : "btn btn-sm btn-ghost"}
                onClick={() => { setTab("nps"); setErr(null); }}
              >
                Rate us
              </button>
            </div>

            {tab === "feedback" ? (
              <div className="field">
                <label>What&apos;s on your mind?</label>
                <textarea
                  className="input"
                  rows={4}
                  value={message}
                  onChange={(e) => setMessage(e.target.value)}
                  placeholder="Tell us what's working, or what's not…"
                />
              </div>
            ) : (
              <div className="field">
                <label>How likely are you to recommend TradeBridge? (0 = not likely, 10 = very likely)</label>
                <div className="row" style={{ gap: "0.3rem", flexWrap: "wrap" }}>
                  {Array.from({ length: 11 }, (_, i) => i).map((n) => (
                    <button
                      key={n}
                      type="button"
                      className={score === n ? "btn btn-sm btn-primary" : "btn btn-sm btn-ghost"}
                      onClick={() => setScore(n)}
                      style={{ minWidth: "2.1rem", padding: "0.3rem 0" }}
                      aria-pressed={score === n}
                    >
                      {n}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {err && <p className="err">{err}</p>}

            <div className="row" style={{ gap: "0.6rem", justifyContent: "flex-end", marginTop: "0.75rem" }}>
              <button type="button" className="btn btn-ghost" onClick={close} disabled={busy}>Cancel</button>
              <button type="button" className="btn btn-primary" onClick={submit} disabled={busy}>
                {busy ? "Sending…" : "Send"}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
