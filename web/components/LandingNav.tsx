"use client";

import Link from "next/link";
import { useToken } from "@/lib/auth";
import { Icon } from "@/components/Icon";

const LINKS = [
  { href: "/#platform", label: "How it helps" },
  { href: "/#how", label: "How it works" },
  { href: "/marketplace", label: "Suppliers" },
  { href: "/pricing", label: "Pricing" },
  { href: "/register?role=seller", label: "Sell on TradeBridge" },
];

// LandingNav is the public marketing top bar. It is auth-aware: a signed-in
// visitor sees "Dashboard"; everyone else sees Log in / Sign up. Below 720px the
// text links collapse into a menu.
export default function LandingNav() {
  const token = useToken();
  const auth = token ? (
    <Link className="btn btn-sm btn-primary" href="/dashboard">Dashboard</Link>
  ) : (
    <>
      <Link className="btn btn-sm" href="/login">Log in</Link>
      <Link className="btn btn-sm btn-primary" href="/register">Sign up</Link>
    </>
  );
  return (
    <header className="lp-nav">
      <Link href="/" className="brand lp-brand">
        <span className="brand-mark"><Icon name="zap" size={16} strokeWidth={2.2} /></span> TradeBridge
      </Link>
      <nav className="lp-nav-links" aria-label="Main">
        {LINKS.map((l) => (
          <Link key={l.href} href={l.href} className="lp-nav-link">{l.label}</Link>
        ))}
        <span className="lp-nav-auth">{auth}</span>
      </nav>
      <details className="lp-menu">
        <summary className="btn btn-sm" aria-label="Open menu">Menu</summary>
        <nav className="lp-menu-panel" aria-label="Main">
          {LINKS.map((l) => (
            <Link key={l.href} href={l.href}>{l.label}</Link>
          ))}
          <span className="lp-nav-auth" style={{ padding: "0.5rem 0.25rem 0.25rem" }}>{auth}</span>
        </nav>
      </details>
    </header>
  );
}
