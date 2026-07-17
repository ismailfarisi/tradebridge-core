"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { VerifiedBadge } from "@/components/VerifiedBadge";
import { StarRating } from "@/components/StarRating";
import { searchDiscovery, getFacets, type DiscoveryResult, type Facets, type DiscoveryQuery } from "@/lib/discovery";

export function DiscoveryResults({ initialCategory }: { initialCategory?: string }) {
  const [q, setQ] = useState("");
  const [category, setCategory] = useState(initialCategory ?? "");
  const [verified, setVerified] = useState(false);
  const [minRating, setMinRating] = useState(0);
  const [sort, setSort] = useState<DiscoveryQuery["sort"]>("relevance");
  const [results, setResults] = useState<DiscoveryResult[]>([]);
  const [facets, setFacets] = useState<Facets | null>(null);
  const [total, setTotal] = useState(0);
  const [err, setErr] = useState<string | null>(null);

  const query: DiscoveryQuery = { q, category, verified, min_rating: minRating, sort };

  const run = useCallback(() => {
    searchDiscovery(query).then((p) => { setResults(p.results); setTotal(p.total); }).catch((e) => setErr(e.message));
    getFacets(query).then(setFacets).catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, category, verified, minRating, sort]);

  useEffect(() => { run(); }, [run]);

  return (
    <div style={{ display: "grid", gridTemplateColumns: "220px 1fr", gap: "1.5rem" }}>
      <aside className="col" style={{ gap: "1rem" }}>
        <div className="field">
          <label>Category</label>
          <div className="col" style={{ gap: "0.25rem" }}>
            <button className="btn" onClick={() => setCategory("")} aria-pressed={category === ""}>All</button>
            {facets?.categories.map((c) => (
              <button key={c.value} className="btn" aria-pressed={category === c.value}
                onClick={() => setCategory(c.value)}>{c.value} ({c.count})</button>
            ))}
          </div>
        </div>
        <label><input type="checkbox" checked={verified} onChange={(e) => setVerified(e.target.checked)} /> Verified only{facets ? ` (${facets.verified.true})` : ""}</label>
        <div className="field">
          <label>Min rating</label>
          <select className="input" value={minRating} onChange={(e) => setMinRating(Number(e.target.value))}>
            <option value={0}>Any</option><option value={4}>4+</option><option value={3}>3+</option><option value={2}>2+</option>
          </select>
        </div>
      </aside>

      <section className="col" style={{ gap: "1rem" }}>
        <div className="row" style={{ gap: "0.5rem" }}>
          <input className="input" placeholder="Search suppliers, products…" value={q} onChange={(e) => setQ(e.target.value)} style={{ flex: 1 }} />
          <select className="input" value={sort} onChange={(e) => setSort(e.target.value as DiscoveryQuery["sort"])}>
            <option value="relevance">Relevance</option><option value="rating">Rating</option><option value="newest">Newest</option>
          </select>
        </div>
        {err && <p className="err">{err}</p>}
        <p className="muted small">{total} supplier{total === 1 ? "" : "s"}</p>
        {results.map((r) => (
          <Link key={r.company_id} href={`/s/${r.company_id}`} className="card" style={{ display: "block" }}>
            <div className="row" style={{ gap: "0.5rem", alignItems: "center" }}>
              <strong>{r.name}</strong>
              <VerifiedBadge status={r.verification_status} />
              {r.review_count > 0 && <StarRating value={r.avg_rating} count={r.review_count} />}
            </div>
            {r.location && <p className="muted small" style={{ margin: "0.25rem 0" }}>{r.location}</p>}
            <p className="small" style={{ margin: "0.25rem 0" }}>{r.description}</p>
            <div className="row" style={{ gap: "0.35rem", flexWrap: "wrap" }}>
              {r.categories.map((c) => <span key={c} className="badge">{c}</span>)}
            </div>
            {r.matched_catalog && r.matched_catalog.length > 0 && (
              <p className="muted small" style={{ marginTop: "0.35rem" }}>
                Matched: {r.matched_catalog.map((m) => m.name).join(", ")}
              </p>
            )}
          </Link>
        ))}
        {results.length === 0 && !err && <p className="muted">No suppliers match yet.</p>}
      </section>
    </div>
  );
}
