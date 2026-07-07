import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "TradeBridge",
  description: "B2B RFQ marketplace — buyer, seller & admin console",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
