import type { Metadata } from "next";
import { getPublicSeller } from "@/lib/trust";

type Props = {
  params: Promise<{ id: string }>;
  children: React.ReactNode;
};

// The storefront page itself is a client component ("use client"), so it can't
// export `metadata`/`generateMetadata` directly. This server layout wraps it
// to supply per-seller OG/Twitter metadata without touching the client page.
export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { id } = await params;
  try {
    const seller = await getPublicSeller(id);
    const description =
      seller.description ?? `${seller.name} on TradeBridge — verified B2B supplier.`;
    return {
      title: seller.name,
      description,
      openGraph: { title: seller.name, description },
      twitter: { title: seller.name, description },
    };
  } catch {
    const title = "Supplier storefront";
    const description = "Verified B2B supplier profile on TradeBridge.";
    return {
      title,
      description,
      openGraph: { title, description },
      twitter: { title, description },
    };
  }
}

export default function StorefrontLayout({ children }: Props) {
  return <>{children}</>;
}
