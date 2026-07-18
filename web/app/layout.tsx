import type { Metadata } from "next";
import Link from "next/link";
import "./globals.css";
import CookieConsent from "@/components/CookieConsent";

export const metadata: Metadata = {
  title: "TradeBridge",
  description: "B2B RFQ marketplace — buyer, seller & admin console",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>
        {children}
        <footer
          className="small muted"
          style={{
            borderTop: "1px solid var(--border)",
            padding: "1.25rem",
            textAlign: "center",
          }}
        >
          <nav className="row" style={{ justifyContent: "center", gap: "1rem" }}>
            <Link href="/legal/terms">Terms</Link>
            <Link href="/legal/privacy">Privacy</Link>
            <Link href="/legal/cookies">Cookies</Link>
          </nav>
        </footer>
        <CookieConsent />
      </body>
    </html>
  );
}
