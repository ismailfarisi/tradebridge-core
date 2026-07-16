"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useToken } from "@/lib/auth";
import { startCheckout, type Interval, type Plan } from "@/lib/billing";

type Tier = {
  plan: Plan;
  name: string;
  monthly: number; // display price (USD)
  blurb: string;
  features: string[];
  cta: string;
  highlight?: boolean;
};

const TIERS: Tier[] = [
  {
    plan: "free", name: "Free", monthly: 0, blurb: "Get on the marketplace.",
    features: ["25 catalog items", "10 RFQs / month", "2 seats", "Manual quote review"],
    cta: "Current plan",
  },
  {
    plan: "pro", name: "Pro", monthly: 49, blurb: "Let your agent quote 24/7.", highlight: true,
    features: ["500 catalog items", "100 RFQs / month", "5 seats", "Agent autonomy (auto-quoting)"],
    cta: "Upgrade to Pro",
  },
  {
    plan: "business", name: "Business", monthly: 199, blurb: "Scale with your team.",
    features: ["Unlimited catalog", "Unlimited RFQs", "Unlimited seats", "Agent autonomy + everything in Pro"],
    cta: "Upgrade to Business",
  },
];

export default function PricingPage() {
  const token = useToken();
  const router = useRouter();
  const [interval, setInterval] = useState<Interval>("month");
  const [busy, setBusy] = useState<Plan | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const choose = async (plan: Plan) => {
    if (plan === "free") return;
    if (!token) {
      router.push("/register");
      return;
    }
    setBusy(plan);
    setErr(null);
    try {
      const url = await startCheckout(token, plan, interval);
      window.location.href = url;
    } catch (e) {
      setErr(e instanceof Error ? e.message : "checkout failed");
      setBusy(null);
    }
  };

  const price = (m: number) => (interval === "year" ? Math.round(m * 10) : m); // 2 months free

  return (
    <main style={{ maxWidth: 980, margin: "0 auto", padding: "3rem 1.5rem" }}>
      <div style={{ textAlign: "center", marginBottom: "2rem" }}>
        <h1 style={{ fontSize: "2rem" }}>Pricing</h1>
        <p className="muted">Start free. Upgrade when your agent should quote for you.</p>
        <div className="row" style={{ justifyContent: "center", gap: "0.5rem", marginTop: "1rem" }}>
          <button className={`btn btn-sm ${interval === "month" ? "btn-primary" : "btn-ghost"}`} onClick={() => setInterval("month")}>Monthly</button>
          <button className={`btn btn-sm ${interval === "year" ? "btn-primary" : "btn-ghost"}`} onClick={() => setInterval("year")}>Annual · 2 months free</button>
        </div>
      </div>

      {err && <div className="notice err" style={{ marginBottom: "1rem" }}>{err}</div>}

      <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", gap: "1rem" }}>
        {TIERS.map((t) => (
          <div key={t.plan} className="card" style={t.highlight ? { borderColor: "var(--primary)" } : undefined}>
            {t.highlight && <span className="badge badge-info" style={{ marginBottom: "0.6rem" }}>Most popular</span>}
            <h2 style={{ fontSize: "1.25rem" }}>{t.name}</h2>
            <p className="muted" style={{ margin: "0.2rem 0 0.75rem" }}>{t.blurb}</p>
            <div style={{ fontSize: "1.8rem", fontWeight: 700 }}>
              ${price(t.monthly)}<span className="faint" style={{ fontSize: "0.9rem", fontWeight: 400 }}>/{interval === "year" ? "yr" : "mo"}</span>
            </div>
            <ul style={{ listStyle: "none", padding: 0, margin: "1rem 0", display: "flex", flexDirection: "column", gap: "0.4rem" }}>
              {t.features.map((f) => <li key={f} className="row" style={{ gap: "0.5rem" }}><span style={{ color: "var(--success)" }}>✓</span> {f}</li>)}
            </ul>
            <button
              className={`btn ${t.highlight ? "btn-primary" : ""}`}
              style={{ width: "100%" }}
              disabled={t.plan === "free" || busy !== null}
              onClick={() => choose(t.plan)}
            >
              {busy === t.plan ? "Redirecting…" : t.cta}
            </button>
          </div>
        ))}
      </div>

      <p className="muted small" style={{ textAlign: "center", marginTop: "1.5rem" }}>
        Already have an account? <Link href="/settings/billing">Manage billing</Link>
      </p>
    </main>
  );
}
