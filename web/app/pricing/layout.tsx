import type { Metadata } from "next";

// The pricing page itself is a client component ("use client"), so it can't
// export `metadata` directly. This server layout supplies it instead.
export const metadata: Metadata = {
  title: "Pricing",
  description: "TradeBridge plans and pricing for buyers and sellers.",
};

export default function PricingLayout({ children }: { children: React.ReactNode }) {
  return <>{children}</>;
}
