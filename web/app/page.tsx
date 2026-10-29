import type { Metadata } from "next";
import Link from "next/link";
import LandingNav from "@/components/LandingNav";
import SiteFooter from "@/components/SiteFooter";
import { Icon } from "@/components/Icon";

export const metadata: Metadata = {
  title: "Suppliers quote, your agent negotiates",
  description:
    "Tell TradeBridge what your business needs. UAE suppliers send quotes, and an AI agent bargains with each of them inside the budget you set. You approve the deal.",
  openGraph: {
    title: "TradeBridge: stop haggling with suppliers",
    description: "Suppliers quote. Your agent bargains with every one of them inside your budget. You approve the deal.",
  },
};

type RowState = "agreed" | "countered" | "walked" | "waiting";

const FLOOR: { supplier: string; opened: number; latest: number | null; round: number; state: RowState }[] = [
  { supplier: "Al Noor Packaging", opened: 10400, latest: 8650, round: 3, state: "agreed" },
  { supplier: "Gulf Print House", opened: 9900, latest: 8900, round: 3, state: "countered" },
  { supplier: "Desert Box Trading", opened: 11200, latest: 10100, round: 2, state: "walked" },
  { supplier: "Creekside Supplies", opened: 9600, latest: null, round: 1, state: "waiting" },
];

const STATE_LABEL: Record<RowState, { text: string; cls: string }> = {
  agreed: { text: "Best deal", cls: "badge badge-success" },
  countered: { text: "Countered", cls: "badge badge-info" },
  walked: { text: "Over budget", cls: "badge badge-danger" },
  waiting: { text: "Waiting", cls: "badge" },
};

const aed = (n: number) => `AED ${n.toLocaleString("en-US")}`;

const PROOFS = [
  {
    title: "It haggles so you don't",
    body: "Your agent counters every quote, round after round, and uses rival offers as leverage without telling suppliers who else is bidding.",
  },
  {
    title: "It never goes past your limit",
    body: "You set the price you want, the most you'll pay and when you need delivery. Those limits are enforced on our servers, not left to the AI.",
  },
  {
    title: "You approve, and you see the saving",
    body: "Every counter-offer is recorded with the reason it was made. Approve the deal in one tap and the purchase order goes to the supplier.",
  },
];

const STEPS = [
  { title: "Tell us what you need", body: "Type it like a message to a supplier: what, how many, by when." },
  { title: "Suppliers send quotes", body: "Verified suppliers reply with prices and delivery times." },
  { title: "Your agent bargains", body: "It counters each one inside your budget and plays the offers against each other." },
  { title: "You approve the deal", body: "Pick the agent's best deal and the purchase order goes out." },
];

const TRUST = [
  "Suppliers verify their business domain",
  "Every move your agent makes is recorded with its reason",
  "Your agent can't go over your limit",
  "Your data stays yours: export or delete it anytime",
  "Ratings, reviews and disputes built in",
  "We never handle your payments",
];

const TIERS = [
  { name: "Free", price: "$0", blurb: "Get quotes and approve every deal yourself.", features: ["Requests in plain language", "Supplier matching", "Agent negotiation with your approval"] },
  { name: "Pro", price: "$49", blurb: "Let your agent close deals inside your limits.", features: ["Everything in Free", "Auto-accept within your limits", "Savings reports"], highlight: true },
  { name: "Business", price: "$199", blurb: "For a business with several people buying.", features: ["Everything in Pro", "Team seats and roles", "Priority support"] },
];

export default function Landing() {
  return (
    <div className="lp">
      <div className="lp-hero-bg">
        <div className="lp-wrap">
          <LandingNav />
          <section className="lp-hero">
            <div>
              <h1 className="lp-title">Stop haggling with suppliers. Your agent does it.</h1>
              <p className="lp-sub">
                Tell TradeBridge what your business needs. Suppliers across the UAE send quotes, and your agent bargains
                with each of them inside the budget you set. You approve the deal, and see exactly how much it saved.
              </p>
              <div className="lp-cta-row">
                <Link className="btn btn-primary lp-btn-lg" href="/register">
                  Get quotes free <Icon name="arrowRight" size={16} />
                </Link>
                <Link className="btn lp-btn-lg" href="/register?role=seller">
                  I&apos;m a supplier
                </Link>
              </div>
              <p className="lp-reassure">Free to start · No card needed · Nothing is bought without your OK</p>
            </div>

            <figure className="lp-floor" aria-label="Example negotiation">
              <div className="lp-floor-head">
                <div>
                  <strong>2,000 printed takeaway boxes</strong>
                  <div className="faint small">4 suppliers · budget <span className="num">{aed(9000)}</span></div>
                </div>
                <span className="badge badge-warning">Needs your OK</span>
              </div>
              <div className="lp-floor-row lp-floor-cols" aria-hidden>
                <span>Supplier</span><span>Opened at</span><span>Now</span><span>Round</span><span>Status</span>
              </div>
              {FLOOR.map((r) => (
                <div key={r.supplier} className={`lp-floor-row${r.state === "agreed" ? " lp-floor-best" : ""}`}>
                  <span className="lp-floor-name">{r.supplier}</span>
                  <span className="num lp-floor-was" data-label="Opened at">{aed(r.opened)}</span>
                  <span className="num lp-floor-now" data-label="Now">{r.latest ? aed(r.latest) : "—"}</span>
                  <span className="num faint lp-floor-round" data-label="Round">{r.round}</span>
                  <span className="lp-floor-state"><span className={STATE_LABEL[r.state].cls}>{STATE_LABEL[r.state].text}</span></span>
                </div>
              ))}
              <div className="lp-floor-agent">
                <span className="lp-floor-bot"><Icon name="bot" size={15} /></span>
                <p>Al Noor came down from AED 10,400 to AED 8,650 with 6-day delivery, under your budget. Approve it?</p>
              </div>
              <figcaption className="lp-floor-foot">
                <span className="faint small">Example negotiation</span>
                <span className="num lp-floor-saved">Saved {aed(1750)} (16.8%)</span>
              </figcaption>
            </figure>
          </section>
        </div>
      </div>

      <section className="lp-section" id="platform">
        <div className="lp-wrap lp-proofs">
          <div>
            <h2 className="lp-section-title">Haggling eats your day. Hand it over.</h2>
            <p className="lp-lead">Most small purchases never get negotiated because nobody has the time. Your agent negotiates every one, the same way every time, inside rules you control.</p>
          </div>
          <ul className="lp-proof-list">
            {PROOFS.map((p) => (
              <li key={p.title}>
                <h3>{p.title}</h3>
                <p>{p.body}</p>
              </li>
            ))}
          </ul>
        </div>
      </section>

      <section className="lp-section lp-section-alt" id="how">
        <div className="lp-wrap">
          <h2 className="lp-section-title">From a message to a purchase order</h2>
          <ol className="lp-steps">
            {STEPS.map((s) => (
              <li key={s.title} className="lp-step">
                <h3>{s.title}</h3>
                <p>{s.body}</p>
              </li>
            ))}
          </ol>
        </div>
      </section>

      <section className="lp-section">
        <div className="lp-wrap lp-trust">
          <div>
            <h2 className="lp-section-title">Your money, your rules</h2>
            <p className="lp-lead">Your agent works to the limits you give it, not a black box. Every counter-offer is recorded with the reason it was made.</p>
          </div>
          <ul className="lp-trust-list">
            {TRUST.map((t) => (
              <li key={t}><Icon name="checkCircle" size={18} /> {t}</li>
            ))}
          </ul>
        </div>
      </section>

      <section className="lp-section lp-section-alt" id="pricing">
        <div className="lp-wrap">
          <div className="lp-center">
            <h2 className="lp-section-title">Free to start. Upgrade when it&apos;s saving you money.</h2>
            <p className="lp-lead">Plans are billed monthly in US dollars.</p>
          </div>
          <div className="lp-tiers">
            {TIERS.map((t) => (
              <div key={t.name} className={`card lp-tier${t.highlight ? " lp-tier-hl" : ""}`}>
                <div className="lp-tier-name">{t.name}</div>
                <div className="lp-tier-price num">{t.price}<span className="muted small" style={{ fontWeight: 500 }}> / month</span></div>
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

      <section className="lp-wrap" style={{ padding: "4rem 1.5rem" }}>
        <div className="lp-cta">
          <h2 className="lp-section-title" style={{ color: "#fff" }}>Send your first request today</h2>
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
