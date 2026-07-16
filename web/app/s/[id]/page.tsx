"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { ReportBlockActions } from "@/components/ReportBlockActions";
import { VerifiedBadge } from "@/components/VerifiedBadge";
import { getPublicSeller, type PublicSeller } from "@/lib/trust";

export default function StorefrontPage() {
  const { id } = useParams<{ id: string }>();
  const [seller, setSeller] = useState<PublicSeller | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    getPublicSeller(id).then(setSeller).catch((e) => setErr(e instanceof Error ? e.message : "not found"));
  }, [id]);

  if (err) {
    return <main style={{ maxWidth: 760, margin: "3rem auto", padding: "0 1rem" }}><h1>Seller not found</h1><p className="muted">{err}</p></main>;
  }
  if (!seller) {
    return <main style={{ maxWidth: 760, margin: "3rem auto", padding: "0 1rem" }}><p className="muted">Loading…</p></main>;
  }

  return (
    <main style={{ maxWidth: 760, margin: "3rem auto", padding: "0 1rem" }}>
      <div className="row" style={{ gap: "0.6rem", alignItems: "center" }}>
        <h1 style={{ margin: 0 }}>{seller.name}</h1>
        <VerifiedBadge status={seller.verification_status} />
      </div>
      {seller.verified_domain && (
        <p className="muted">Verified domain: {seller.verified_domain}</p>
      )}

      <section className="card" style={{ marginTop: "1rem" }}>
        <h2 style={{ marginTop: 0 }}>About</h2>
        <p>{seller.description || "No description yet."}</p>
        <div className="row" style={{ gap: "0.4rem", flexWrap: "wrap" }}>
          {seller.categories.map((c) => <span key={c} className="badge">{c}</span>)}
        </div>
        {seller.location && <p className="muted" style={{ marginTop: "0.5rem" }}>{seller.location}</p>}
      </section>

      <section className="card" style={{ marginTop: "1rem" }}>
        <h2 style={{ marginTop: 0 }}>Catalog</h2>
        {seller.catalog.length === 0 ? (
          <p className="muted">No catalog items published.</p>
        ) : (
          <ul>
            {seller.catalog.map((it, i) => (
              <li key={i}><strong>{it.name}</strong> <span className="muted">({it.category})</span>{it.summary ? ` — ${it.summary}` : ""}</li>
            ))}
          </ul>
        )}
      </section>

      <section className="card" style={{ marginTop: "1rem" }}>
        <h2 style={{ marginTop: 0 }}>Reputation</h2>
        <p className="muted">Ratings & reviews coming soon.</p>
      </section>

      <ReportBlockActions companyId={seller.company_id} />
    </main>
  );
}
