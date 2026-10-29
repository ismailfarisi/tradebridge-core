"use client";

import Link from "next/link";
import { useToken } from "@/lib/auth";
import { Icon } from "@/components/Icon";

// LandingNav is the public marketing top bar (A1 wireframe). It is auth-aware:
// a signed-in visitor sees "Dashboard"; everyone else sees Log in / Sign up.
export default function LandingNav() {
  const token = useToken();
  return (
    <header className="lp-nav">
      <Link href="/" className="brand lp-brand">
        <span className="brand-mark"><Icon name="zap" size={16} strokeWidth={2.2} /></span> TradeBridge
      </Link>
      <nav className="lp-nav-links">
        <a href="#platform" className="lp-nav-link">Platform</a>
        <a href="#how" className="lp-nav-link">How it works</a>
        <Link href="/marketplace" className="lp-nav-link">Suppliers</Link>
        <Link href="/pricing" className="lp-nav-link">Pricing</Link>
        <Link href="/register?role=seller" className="lp-nav-link">Sell</Link>
        {token ? (
          <Link className="btn btn-sm btn-primary" href="/dashboard">Dashboard</Link>
        ) : (
          <>
            <Link className="btn btn-sm" href="/login">Log in</Link>
            <Link className="btn btn-sm btn-primary" href="/register">Sign up</Link>
          </>
        )}
      </nav>
    </header>
  );
}
