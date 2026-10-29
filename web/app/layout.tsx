import type { Metadata } from "next";
import { Inter } from "next/font/google";
import "./globals.css";
import CookieConsent from "@/components/CookieConsent";

const inter = Inter({ subsets: ["latin"], display: "swap", variable: "--font-inter" });

const SITE_URL = "https://switeaz.com";
const SITE_DESCRIPTION = "B2B RFQ marketplace — buyer, seller & admin console";

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: {
    default: "TradeBridge",
    template: "%s — TradeBridge",
  },
  description: SITE_DESCRIPTION,
  openGraph: {
    title: "TradeBridge",
    description: SITE_DESCRIPTION,
    url: SITE_URL,
    siteName: "TradeBridge",
    images: [{ url: "/opengraph-image" }],
    type: "website",
  },
  twitter: {
    card: "summary_large_image",
    title: "TradeBridge",
    description: SITE_DESCRIPTION,
    images: ["/opengraph-image"],
  },
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" className={inter.variable}>
      <body>
        {children}
        <CookieConsent />
      </body>
    </html>
  );
}
