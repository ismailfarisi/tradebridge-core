import { DiscoveryResults } from "@/components/DiscoveryResults";

export default function MarketplacePage() {
  return (
    <>
      <h1>Find suppliers</h1>
      <p className="muted">Search verified suppliers by need, ranked by relevance and trust.</p>
      <DiscoveryResults />
    </>
  );
}
