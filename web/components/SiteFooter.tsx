import Link from "next/link";

export default function SiteFooter() {
  return (
    <footer
      className="small muted"
      style={{
        borderTop: "1px solid var(--border)",
        padding: "1.5rem 1.25rem 3.5rem",
        textAlign: "center",
      }}
    >
      <nav className="row" style={{ justifyContent: "center", gap: "1rem", flexWrap: "wrap" }}>
        <Link href="/marketplace">Suppliers</Link>
        <Link href="/pricing">Pricing</Link>
        <Link href="/register?role=seller">Sell on TradeBridge</Link>
        <Link href="/legal/terms">Terms</Link>
        <Link href="/legal/privacy">Privacy</Link>
        <Link href="/legal/cookies">Cookies</Link>
      </nav>
    </footer>
  );
}
