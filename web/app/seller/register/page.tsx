"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { me, register, setToken, upsertProfile } from "@/lib/api";
import { parseCategories, sellerLandingPath } from "@/lib/seller";
import { AuthWrap } from "../../login/page";

type SellerType = "seller" | "both";

const TYPES: { value: SellerType; label: string; hint: string }[] = [
  { value: "seller", label: "Supplier", hint: "Receive matched RFQs and quote buyers" },
  { value: "both", label: "Supplier and buyer", hint: "Also post your own RFQs" },
];

// Supplier sign-up. Creates a seller company and, when the seller says what
// they supply, publishes the supplier profile in the same step so RFQ
// matching can find them straight away.
export default function SellerRegisterPage() {
  const router = useRouter();
  const [companyName, setCompanyName] = useState("");
  const [companyType, setCompanyType] = useState<SellerType>("seller");
  const [supplies, setSupplies] = useState("");
  const [location, setLocation] = useState("Dubai");
  const [description, setDescription] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (password.length < 8) {
      setErr("Password must be at least 8 characters.");
      return;
    }
    const categories = parseCategories(supplies);
    if (categories.length === 0) {
      setErr("Add at least one product category you supply.");
      return;
    }
    setBusy(true);
    setErr(null);
    let token: string;
    try {
      ({ token } = await register({
        company_name: companyName,
        company_type: companyType,
        email,
        password,
      }));
      setToken(token);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "registration failed");
      setBusy(false);
      return;
    }
    // The account exists from here on. If the profile save fails, send the
    // seller to the profile page to finish it rather than failing sign-up.
    try {
      await upsertProfile(token, { description: description.trim(), categories, location: location.trim() });
    } catch {
      router.replace("/seller/profile");
      return;
    }
    try {
      router.replace(sellerLandingPath(await me(token)));
    } catch {
      router.replace("/seller/rfqs");
    }
  };

  return (
    <AuthWrap
      title="Create a supplier account"
      alt={
        <>
          Already selling on TradeBridge? <Link href="/seller/login">Sign in</Link>
          <br />
          Buying instead? <Link href="/register">Create a buyer account</Link>
        </>
      }
    >
      <p className="muted small" style={{ marginTop: "-0.5rem" }}>
        Buyers describe what they need. We match their RFQs to suppliers by category.
      </p>
      <form className="col" onSubmit={submit}>
        <div className="field">
          <label htmlFor="sr-company">Company name</label>
          <input id="sr-company" className="input" autoComplete="organization" value={companyName} onChange={(e) => setCompanyName(e.target.value)} required />
        </div>
        <fieldset className="field" style={{ border: 0, padding: 0, margin: 0 }}>
          <legend style={{ padding: 0, marginBottom: "0.35rem" }}>Account type</legend>
          <div className="col" style={{ gap: "0.4rem" }}>
            {TYPES.map((t) => (
              <label
                key={t.value}
                className="row"
                style={{
                  gap: "0.6rem",
                  padding: "0.55rem 0.7rem",
                  border: `1px solid ${companyType === t.value ? "var(--primary)" : "var(--border-2)"}`,
                  borderRadius: 8,
                  cursor: "pointer",
                }}
              >
                <input type="radio" name="ctype" checked={companyType === t.value} onChange={() => setCompanyType(t.value)} />
                <span>
                  <strong>{t.label}</strong>
                  <span className="muted small" style={{ display: "block" }}>{t.hint}</span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>
        <div className="field">
          <label htmlFor="sr-supplies">What do you supply?</label>
          <input
            id="sr-supplies"
            className="input"
            placeholder="e.g. packaging, cartons, pallets"
            value={supplies}
            onChange={(e) => setSupplies(e.target.value)}
            required
          />
          <span className="muted small">Comma-separated categories. RFQs are matched on these.</span>
        </div>
        <div className="field">
          <label htmlFor="sr-location">Location</label>
          <input id="sr-location" className="input" value={location} onChange={(e) => setLocation(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="sr-desc">Short description <span className="muted small">(optional)</span></label>
          <textarea id="sr-desc" className="input" rows={2} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Corrugated boxes and custom printed cartons, next-day delivery in the UAE." />
        </div>
        <div className="field">
          <label htmlFor="sr-email">Work email</label>
          <input id="sr-email" className="input" type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </div>
        <div className="field">
          <label htmlFor="sr-password">Password</label>
          <input id="sr-password" className="input" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
        </div>
        {err && <p className="err">{err}</p>}
        <button className="btn btn-primary" disabled={busy}>{busy ? "Creating…" : "Create supplier account"}</button>
      </form>
    </AuthWrap>
  );
}
