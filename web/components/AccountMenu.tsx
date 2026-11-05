"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import type { Me } from "@/lib/api";
import { ACCOUNT_LINKS } from "@/lib/nav";
import { Icon } from "@/components/Icon";
import { openFeedback } from "@/components/FeedbackWidget";

export default function AccountMenu({ me, initials, onSignOut }: { me: Me; initials: string; onSignOut: () => void }) {
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement | null>(null);
  const btn = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        btn.current?.focus();
      }
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const close = () => setOpen(false);

  return (
    <div ref={box} className="acct">
      <button
        ref={btn}
        type="button"
        className="acct-btn"
        aria-expanded={open}
        aria-controls="acct-menu"
        aria-label={`Account menu for ${me.company_name ?? "your company"}`}
        onClick={() => setOpen((o) => !o)}
      >
        <span className="avatar" aria-hidden>{initials}</span>
        <Icon name="chevronDown" size={14} />
      </button>
      {open && (
        <div id="acct-menu" className="acct-menu">
          <div className="acct-head">
            <strong className="truncate">{me.company_name ?? "Your company"}</strong>
            <span className="small muted" style={{ textTransform: "capitalize" }}>{me.company_type ?? "company"} · {me.role}</span>
          </div>
          {ACCOUNT_LINKS.map((l) => (
            <Link key={l.href} href={l.href} className="acct-link" onClick={close}>
              <Icon name={l.icon} size={16} /> {l.label}
            </Link>
          ))}
          <button type="button" className="acct-link" onClick={() => { close(); openFeedback(); }}>
            <Icon name="chat" size={16} /> Send feedback
          </button>
          <div className="acct-sep" />
          <button type="button" className="acct-link" onClick={onSignOut}>
            <Icon name="logout" size={16} /> Sign out
          </button>
        </div>
      )}
    </div>
  );
}
