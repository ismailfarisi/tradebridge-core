import { DiscoveryResults } from "@/components/DiscoveryResults";

export default async function CategoryPage({ params }: { params: Promise<{ category: string }> }) {
  const { category } = await params;
  const decoded = decodeURIComponent(category);
  return (
    <>
      <h1 style={{ textTransform: "capitalize" }}>{decoded}</h1>
      <p className="muted">Suppliers in {decoded}.</p>
      <DiscoveryResults initialCategory={decoded} />
    </>
  );
}
