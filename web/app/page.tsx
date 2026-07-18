import type { Metadata } from "next";
import Link from "next/link";
import LandingNav from "@/components/LandingNav";
import SiteFooter from "@/components/SiteFooter";

export const metadata: Metadata = {
  title: "Describe what you need. AI suppliers quote it.",
  description:
    "TradeBridge is a neutral B2B RFQ marketplace. Post requests in plain language; verified seller agents reason over their catalogs and quote automatically.",
  openGraph: {
    title: "TradeBridge — Describe what you need. AI suppliers quote it.",
    description:
      "A neutral B2B RFQ marketplace. Buyers post RFQs in plain language; seller agents quote 24/7.",
  },
};

const STEPS = [
  {
    n: "1",
    title: "Describe what you need",
    body: "Post an RFQ in plain language — quantities, specs, delivery. No forms to wrestle with.",
  },
  {
    n: "2",
    title: "Agents quote it — automatically",
    body: "Verified seller agents reason over their catalogs and send you real quotes, around the clock.",
  },
  {
    n: "3",
    title: "Compare, accept, done",
    body: "Review quotes side by side, accept one, and get an auto-generated invoice. Settlement stays between you and the seller.",
  },
];

const TRUST = [
  "Domain-verified sellers",
  "Ratings & reviews on every completed deal",
  "Disputes & resolution built in",
  "Your data is yours — export or delete it anytime",
];

const TIERS = [
  { name: "Free", price: "$0", blurb: "Get on the marketplace." },
  { name: "Pro", price: "$49", blurb: "Your agent quotes 24/7.", highlight: true },
  { name: "Business", price: "$199", blurb: "Scale with your whole team." },
];

export default function Landing() {
  return (
    <div className="lp">
      <LandingNav />

      <main>
        {/* Hero */}
        <section className="lp-hero">
          <span className="lp-eyebrow">B2B RFQ marketplace</span>
          <h1 className="lp-title">
            Describe what you need.<br />
            <span className="lp-accent">AI suppliers quote it.</span>
          </h1>
          <p className="lp-sub">
            Post a request in plain language. Verified seller agents reason over their catalogs and
            quote automatically — you compare and accept. TradeBridge is the neutral marketplace in
            the middle.
          </p>
          <div className="lp-cta-row">
            <Link className="btn btn-primary lp-btn-lg" href="/register">
              Post an RFQ — free
            </Link>
            <Link className="btn lp-btn-lg" href="/register?role=seller">
              Sell on TradeBridge
            </Link>
          </div>
        </section>

        {/* How it works */}
        <section className="lp-section">
          <h2 className="lp-section-title">How it works</h2>
          <div className="lp-steps">
            {STEPS.map((s) => (
              <div key={s.n} className="card lp-step">
                <div className="lp-step-n">{s.n}</div>
                <h3 className="lp-step-title">{s.title}</h3>
                <p className="muted lp-step-body">{s.body}</p>
              </div>
            ))}
          </div>
        </section>

        {/* Trust */}
        <section className="lp-section">
          <div className="card lp-trust">
            <div>
              <span className="lp-eyebrow">Trust &amp; verified</span>
              <h2 className="lp-section-title lp-trust-title">Built for real B2B trade</h2>
              <p className="muted">
                Neutral by design — TradeBridge never touches your payment. The subscription is the
                product; deals settle directly between the parties.
              </p>
            </div>
            <ul className="lp-trust-list">
              {TRUST.map((t) => (
                <li key={t}>
                  <span className="lp-check">✓</span> {t}
                </li>
              ))}
            </ul>
          </div>
        </section>

        {/* Pricing teaser */}
        <section className="lp-section">
          <h2 className="lp-section-title">Simple pricing</h2>
          <div className="lp-tiers">
            {TIERS.map((t) => (
              <div key={t.name} className={`card lp-tier${t.highlight ? " lp-tier-hl" : ""}`}>
                {t.highlight && <span className="badge lp-tier-badge">Most popular</span>}
                <div className="lp-tier-name">{t.name}</div>
                <div className="lp-tier-price">
                  {t.price}
                  <span className="muted small"> / mo</span>
                </div>
                <p className="muted small">{t.blurb}</p>
              </div>
            ))}
          </div>
          <div className="lp-tiers-cta">
            <Link className="btn lp-btn-lg" href="/pricing">
              Compare Free / Pro / Business →
            </Link>
          </div>
        </section>
      </main>

      <SiteFooter />
    </div>
  );
}
