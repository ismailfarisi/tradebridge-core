import { CORE_URL } from "./api";

export type DiscoveryResult = {
  company_id: string;
  name: string;
  verification_status: string;
  verified_domain: string | null;
  description: string | null;
  categories: string[];
  location: string | null;
  verified: boolean;
  avg_rating: number;
  review_count: number;
  score: number;
  matched_catalog?: { name: string; category: string }[];
};
export type DiscoveryPage = {
  results: DiscoveryResult[];
  page: number;
  per_page: number;
  total: number;
};
export type Facets = {
  categories: { value: string; count: number }[];
  verified: { true: number; false: number };
  rating_buckets: { min: number; count: number }[];
};
export type DiscoveryQuery = {
  q?: string;
  category?: string;
  location?: string;
  verified?: boolean;
  min_rating?: number;
  sort?: "relevance" | "rating" | "newest";
  page?: number;
  per_page?: number;
};

function qs(p: DiscoveryQuery): string {
  const s = new URLSearchParams();
  if (p.q) s.set("q", p.q);
  if (p.category) s.set("category", p.category);
  if (p.location) s.set("location", p.location);
  if (p.verified) s.set("verified", "true");
  if (p.min_rating) s.set("min_rating", String(p.min_rating));
  if (p.sort) s.set("sort", p.sort);
  if (p.page) s.set("page", String(p.page));
  if (p.per_page) s.set("per_page", String(p.per_page));
  return s.toString();
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${CORE_URL}${path}`, { cache: "no-store" });
  const text = await res.text();
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return (text ? JSON.parse(text) : undefined) as T;
}

export const searchDiscovery = (p: DiscoveryQuery) =>
  get<DiscoveryPage>(`/public/v1/discovery?${qs(p)}`);
export const getFacets = (p: DiscoveryQuery) =>
  get<Facets>(`/public/v1/discovery/facets?${qs(p)}`);
