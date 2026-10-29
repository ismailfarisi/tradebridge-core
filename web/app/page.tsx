import type { Metadata } from "next";
import Link from "next/link";
import LandingNav from "@/components/LandingNav";
import SiteFooter from "@/components/SiteFooter";
import { Icon, type IconName } from "@/components/Icon";

export const metadata: Metadata = {
  title: "Autonomous procurement negotiation",
  description:
    "TradeBridge sources suppliers from a plain-language RFQ, then an AI agent negotiates every quote within your limits — and shows you exactly what it saved.",
  openGraph: {
    title: "TradeBridge — Your AI procurement negotiator",
    description: "Post an RFQ. Supplier agents quote. Your agent negotiates every one of them, 24/7.",
  },
};

const FEATURES: { icon: IconName; title: string; body: string }[] = [
  { icon: "chat", title: "RFQs in plain language", body: "Describe what you need like you would to a colleague. Requirements are extracted and matched to verified suppliers instantly." },
  { icon: "handshake", title: "Autonomous negotiation", body: "Set a target, a walk-away price and delivery limits. Your agent counters every supplier, round after round, and never exceeds your mandate." },
  { icon: "scale", title: "Competitive leverage", body: "Rival quotes are used as leverage automatically, so every supplier is pushed toward the best price in the market — without revealing who is bidding." },
  { icon: "target", title: "Price and delivery, balanced", body: "Weight price against lead time. Each offer gets a deal score, so a slightly pricier supplier who delivers on time can still win." },
  { icon: "shield", title: "You stay in control", body: "Auto-accept within limits, or approve each deal with one click. Every move is logged with the agent's reasoning for a full audit trail." },
  { icon: "trending", title: "Savings you can prove", body: "Spend, savings vs opening quotes, and supplier performance tracked automatically — ready for your next finance review." },
];

const STEPS = [
  { title: "Describe the need", body: "Post an RFQ in chat. Quantities, specs and deadlines are captured for you." },
  { title: "Suppliers quote", body: "Verified seller agents reason over their catalogs and quote within minutes." },
  { title: "Your agent negotiates", body: "It counters each supplier inside your mandate and plays offers against each other." },
  { title: "Approve and order", body: "The best deal is accepted or held for approval, and the purchase order is issued." },
];

const TRUST = [
  "Domain-verified suppliers",
  "Every agent move logged with reasoning",
  "Tenant-isolated data, export or delete anytime",
  "Disputes, ratings and reviews built in",
  "Hard walk-away limits, never exceeded",
  "Neutral marketplace — we never touch payments",
];

const TIERS = [
  { name: "Free", price: "$0", blurb: "Source and compare quotes.", features: ["Plain-language RFQs", "Supplier matching", "Agent negotiation with approval"] },
  { name: "Pro", price: "$49", blurb: "Hands-off negotiation.", features: ["Everything in Free", "Auto-accept within your limits", "Savings analytics"], highlight: true },
  { name: "Business", price: "$199", blurb: "Procurement for the whole team.", features: ["Everything in Pro", "Team seats and roles", "Priority support"] },
];

export default function Landing() {
  return (
    <div className="lp">
      <div className="lp-hero-bg">
        <div className="lp-wrap">
          <LandingNav />
          <section className="lp-hero">
            <div>
              <span className="lp-eyebrow"><Icon name="sparkles" size={14} /> AI procurement negotiation</span>
              <h1 className="lp-title">
                Every quote, negotiated.<br />
                <span className="lp-accent">Automatically.</span>
              </h1>
              <p className="lp-sub">
                Post what you need in plain language. Suppliers&apos; agents quote, and your TradeBridge agent negotiates
                each of them within the limits you set — then shows you exactly what it saved.
              </p>
              <div className="lp-cta-row">
                <Link className="btn btn-primary lp-btn-lg" href="/register">
                  Start negotiating — free <Icon name="arrowRight" size={16} />
                </Link>
                <Link className="btn lp-btn-lg" href="/register?role=seller">
                  I&apos;m a supplier
                </Link>
              </div>
              <div className="lp-proof">
                <div><b>24/7</b><span>agent coverage</span></div>
                <div><b>Minutes</b><span>from RFQ to quotes</span></div>
                <div><b>100%</b><span>moves audited</span></div>
              </div>
            </div>

            <div className="lp-demo" aria-label="Example negotiation">
              <div className="lp-demo-head">
                <div>
                  <strong>20,000 printed mailer boxes</strong>
                  <div className="faint small">3 suppliers · target $228,000</div>
                </div>
                <span className="live">Agent live</span>
              </div>
              <div className="lp-msg seller"><span className="who">GP</span><div className="bub">Our price is <b>$255,468</b>, delivery in 9 days.</div></div>
              <div className="lp-msg agent"><span className="who"><Icon name="bot" size={14} /></span><div className="bub">We have a competing offer at $239,716. We can award today at <b>$231,500</b> with 8-day delivery.</div></div>
              <div className="lp-msg seller"><span className="who">GP</span><div className="bub">We can meet you at <b>$236,900</b>.</div></div>
              <div className="lp-msg agent"><span className="who"><Icon name="bot" size={14} /></span><div className="bub">Within mandate. Best deal score across 3 suppliers — awarding.</div></div>
              <div className="lp-deal">
                <span className="row" style={{ gap: "0.5rem" }}><Icon name="checkCircle" size={18} /> <strong>Deal closed</strong></span>
                <span className="num" style={{ color: "var(--success)", fontWeight: 700 }}>−$18,568 (7.3%)</span>
              </div>
            </div>
          </section>
        </div>
      </div>

      <div className="lp-wrap">
        <div className="lp-logos" aria-hidden>
          <span>NORTHWIND</span><span>GULFPACK</span><span>EMIRATES BOX</span><span>DESERT PALLET</span><span>MERIDIAN</span>
        </div>
      </div>

      <section className="lp-section lp-section-alt" id="platform">
        <div className="lp-wrap">
          <div className="lp-center">
            <span className="lp-kicker">Platform</span>
            <h2 className="lp-section-title">A buyer-side agent for the long tail of spend</h2>
            <p className="lp-lead">Most purchases are never negotiated because there&apos;s no time. TradeBridge negotiates all of them, consistently, inside rules you control.</p>
          </div>
          <div className="lp-features">
            {FEATURES.map((f) => (
              <div key={f.title} className="card lp-feature">
                <span className="lp-feature-ic"><Icon name={f.icon} size={20} /></span>
                <h3>{f.title}</h3>
                <p>{f.body}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="lp-section" id="how">
        <div className="lp-wrap">
          <span className="lp-kicker">How it works</span>
          <h2 className="lp-section-title">From request to purchase order, hands-free</h2>
          <div className="lp-steps">
            {STEPS.map((s) => (
              <div key={s.title} className="lp-step">
                <h3>{s.title}</h3>
                <p>{s.body}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="lp-section lp-section-alt">
        <div className="lp-wrap lp-trust">
          <div>
            <span className="lp-kicker">Trust &amp; control</span>
            <h2 className="lp-section-title">Built for real B2B trade</h2>
            <p className="lp-lead">Your agent works on a mandate, not a black box. Limits are enforced on the server, and every counter-offer is recorded with the reason it was made.</p>
          </div>
          <ul className="lp-trust-list">
            {TRUST.map((t) => (
              <li key={t}><Icon name="checkCircle" size={18} /> {t}</li>
            ))}
          </ul>
        </div>
      </section>

      <section className="lp-section">
        <div className="lp-wrap">
          <div className="lp-center">
            <span className="lp-kicker">Pricing</span>
            <h2 className="lp-section-title">Pays for itself on the first deal</h2>
          </div>
          <div className="lp-tiers">
            {TIERS.map((t) => (
              <div key={t.name} className={`card lp-tier${t.highlight ? " lp-tier-hl" : ""}`}>
                {t.highlight && <span className="badge lp-tier-badge">Most popular</span>}
                <div className="lp-tier-name">{t.name}</div>
                <div className="lp-tier-price">{t.price}<span className="muted small" style={{ fontWeight: 500 }}> / month</span></div>
                <p className="muted small" style={{ margin: 0 }}>{t.blurb}</p>
                <ul>
                  {t.features.map((f) => <li key={f}><Icon name="check" size={14} /> {f}</li>)}
                </ul>
              </div>
            ))}
          </div>
          <div className="lp-center" style={{ marginTop: "1.5rem" }}>
            <Link className="btn" href="/pricing">Compare plans <Icon name="arrowRight" size={16} /></Link>
          </div>
        </div>
      </section>

      <section className="lp-wrap" style={{ paddingBottom: "4rem" }}>
        <div className="lp-cta">
          <h2 className="lp-section-title" style={{ color: "#fff" }}>Put your next RFQ on autopilot</h2>
          <p>Free to start. No card required.</p>
          <div className="lp-cta-row" style={{ justifyContent: "center" }}>
            <Link className="btn btn-primary lp-btn-lg" href="/register">Create your account <Icon name="arrowRight" size={16} /></Link>
            <Link className="btn lp-btn-lg" href="/login">Log in</Link>
          </div>
        </div>
      </section>

      <SiteFooter />
    </div>
  );
}
