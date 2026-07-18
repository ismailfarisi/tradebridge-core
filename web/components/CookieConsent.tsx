"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { needsConsent, writeConsent } from "@/lib/consent";

export default function CookieConsent() {
  const [visible, setVisible] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setVisible(needsConsent());
  }, []);

  // Publish the banner's height as a CSS variable so layouts with
  // bottom-anchored content (the chat composer) can reserve space and avoid
  // being overlapped by this fixed bar. Reset to 0 when the banner is gone.
  useEffect(() => {
    const root = document.documentElement;
    if (visible && ref.current) {
      root.style.setProperty("--cookie-banner", `${ref.current.offsetHeight}px`);
    } else {
      root.style.setProperty("--cookie-banner", "0px");
    }
    return () => root.style.setProperty("--cookie-banner", "0px");
  }, [visible]);

  if (!visible) return null;

  const choose = (v: "accepted" | "rejected") => {
    writeConsent(v);
    setVisible(false);
  };

  return (
    <div
      ref={ref}
      role="dialog"
      aria-live="polite"
      aria-label="Cookie consent"
      style={{
        position: "fixed",
        left: 0,
        right: 0,
        bottom: 0,
        zIndex: 1000,
        background: "var(--surface)",
        borderTop: "1px solid var(--border)",
        padding: "1rem 1.25rem",
      }}
    >
      <div
        style={{
          maxWidth: 1080,
          margin: "0 auto",
          display: "flex",
          flexWrap: "wrap",
          gap: "0.75rem 1.25rem",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <p className="small muted" style={{ margin: 0, flex: "1 1 320px" }}>
          We use essential cookies to run TradeBridge, and optional cookies to
          understand usage. See our{" "}
          <Link href="/legal/cookies">cookie policy</Link> to learn more or
          change your choice later.
        </p>
        <div className="row" style={{ flex: "0 0 auto" }}>
          <button
            type="button"
            className="btn btn-sm btn-ghost"
            onClick={() => choose("rejected")}
          >
            Reject
          </button>
          <button
            type="button"
            className="btn btn-sm btn-primary"
            onClick={() => choose("accepted")}
          >
            Accept
          </button>
        </div>
      </div>
    </div>
  );
}
