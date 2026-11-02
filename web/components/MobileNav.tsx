"use client";

import { useRef } from "react";
import Link from "next/link";
import type { Me } from "@/lib/api";
import { ACCOUNT_LINKS, isActive, type NavItem } from "@/lib/nav";
import { Icon } from "@/components/Icon";
import { openFeedback } from "@/components/FeedbackWidget";

const TABS = 4;

// Phone and tablet navigation: the first pages as a bottom tab bar, everything
// else (remaining pages, account, sign out) in a "More" sheet.
export default function MobileNav({
  me,
  items,
  pathname,
  decisions,
  onSignOut,
}: {
  me: Me;
  items: NavItem[];
  pathname: string;
  decisions: number;
  onSignOut: () => void;
}) {
  const sheet = useRef<HTMLDialogElement | null>(null);
  const tabs = items.slice(0, TABS);
  const rest = items.slice(TABS);
  const close = () => sheet.current?.close();
  const moreActive = rest.some((it) => isActive(pathname, it.href)) || ACCOUNT_LINKS.some((l) => isActive(pathname, l.href));

  return (
    <>
      <nav className="tabbar" aria-label="Main navigation">
        {tabs.map((it) => {
          const active = isActive(pathname, it.href);
          const count = it.badge === "needsYou" ? decisions : 0;
          return (
            <Link key={it.href} href={it.href} className={`tb-item${active ? " active" : ""}`} aria-current={active ? "page" : undefined}>
              <span className="tb-ic">
                <Icon name={it.icon} size={20} />
                {count > 0 && <span className="tb-count" aria-hidden>{count}</span>}
              </span>
              <span className="tb-label">{it.short ?? it.label}</span>
              {count > 0 && <span className="sr-only">, {count} need you</span>}
            </Link>
          );
        })}
        <button type="button" className={`tb-item${moreActive ? " active" : ""}`} aria-haspopup="dialog" onClick={() => sheet.current?.showModal()}>
          <span className="tb-ic"><Icon name="menu" size={20} /></span>
          <span className="tb-label">More</span>
        </button>
      </nav>

      <dialog
        ref={sheet}
        className="sheet"
        aria-labelledby="sheet-title"
        onClick={(e) => {
          if (e.target === sheet.current) close();
        }}
      >
        <div className="sheet-head">
          <div style={{ minWidth: 0 }}>
            <strong id="sheet-title" className="truncate" style={{ display: "block" }}>{me.company_name ?? "Your company"}</strong>
            <span className="small muted" style={{ textTransform: "capitalize" }}>{me.company_type ?? "company"} · {me.role}</span>
          </div>
          <button type="button" className="icon-btn" aria-label="Close menu" onClick={close}>
            <Icon name="x" />
          </button>
        </div>
        {rest.map((it) => {
          const active = isActive(pathname, it.href);
          return (
            <Link key={it.href} href={it.href} className={`acct-link${active ? " active" : ""}`} aria-current={active ? "page" : undefined} onClick={close}>
              <Icon name={it.icon} size={18} /> {it.label}
            </Link>
          );
        })}
        <div className="sheet-group">Account</div>
        {ACCOUNT_LINKS.map((l) => {
          const active = isActive(pathname, l.href);
          return (
            <Link key={l.href} href={l.href} className={`acct-link${active ? " active" : ""}`} aria-current={active ? "page" : undefined} onClick={close}>
              <Icon name={l.icon} size={18} /> {l.label}
            </Link>
          );
        })}
        <button type="button" className="acct-link" onClick={() => { close(); openFeedback(); }}>
          <Icon name="chat" size={18} /> Send feedback
        </button>
        <div className="acct-sep" />
        <button type="button" className="acct-link" onClick={() => { close(); onSignOut(); }}>
          <Icon name="logout" size={18} /> Sign out
        </button>
      </dialog>
    </>
  );
}
