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
      root.style.setProperty(
        "--cookie-banner",
        `${ref.current.offsetHeight}px`,
      );
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
      className="cookie-banner"
    >
      <p className="small muted">
        We use essential cookies to run TradeBridge, and optional ones to
        understand usage. You can change this later; see our{" "}
        <Link href="/legal/cookies">cookie policy</Link>.
      </p>
      <div className="row">
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
  );
}
