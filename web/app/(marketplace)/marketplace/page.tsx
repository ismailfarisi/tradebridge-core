import type { Metadata } from "next";
import { DiscoveryResults } from "@/components/DiscoveryResults";

export const metadata: Metadata = {
  title: "Marketplace",
  description: "Search verified B2B suppliers by need, ranked by relevance and trust.",
};

export default function MarketplacePage() {
  return (
    <>
      <h1>Find suppliers</h1>
      <p className="muted">Search verified suppliers by need, ranked by relevance and trust.</p>
      <DiscoveryResults />
    </>
  );
}
