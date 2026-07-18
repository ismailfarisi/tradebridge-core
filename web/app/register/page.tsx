"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { me, register, setToken, type CompanyType } from "@/lib/api";
import { landingPath } from "@/lib/landing";
import { AuthWrap } from "../login/page";

const TYPES: { value: CompanyType; label: string; hint: string }[] = [
  { value: "buyer", label: "Buyer", hint: "Post RFQs and receive quotes" },
  { value: "seller", label: "Seller", hint: "List a catalog and quote via an agent" },
  { value: "both", label: "Both", hint: "Buy and sell on the marketplace" },
];

export default function RegisterPage() {
  const router = useRouter();
  const [companyName, setCompanyName] = useState("");
  const [companyType, setCompanyType] = useState<CompanyType>("buyer");

  // Preselect the account type from ?role= so the landing's "Sell on
  // TradeBridge" CTA lands on a seller-defaulted form (read on the client to
  // avoid a Suspense boundary / hydration mismatch).
  useEffect(() => {
    const role = new URLSearchParams(window.location.search).get("role");
    if (role && TYPES.some((t) => t.value === role)) {
      setCompanyType(role as CompanyType);
    }
  }, []);
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
    setBusy(true);
    setErr(null);
    try {
      const { token } = await register({
        company_name: companyName,
        company_type: companyType,
        email,
        password,
      });
      setToken(token);
      const profile = await me(token);
      router.replace(landingPath(profile));
    } catch (e) {
      setErr(e instanceof Error ? e.message : "registration failed");
      setBusy(false);
    }
  };

  return (
    <AuthWrap title="Create account" alt={<>Already registered? <Link href="/login">Sign in</Link></>}>
      <form className="col" onSubmit={submit}>
        <div className="field">
          <label>Company name</label>
          <input className="input" value={companyName} onChange={(e) => setCompanyName(e.target.value)} required />
        </div>
        <div className="field">
          <label>Account type</label>
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
                <input
                  type="radio"
                  name="ctype"
                  checked={companyType === t.value}
                  onChange={() => setCompanyType(t.value)}
                />
                <span>
                  <strong>{t.label}</strong>
                  <span className="muted small" style={{ display: "block" }}>{t.hint}</span>
                </span>
              </label>
            ))}
          </div>
        </div>
        <div className="field">
          <label>Work email</label>
          <input className="input" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </div>
        <div className="field">
          <label>Password</label>
          <input className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
        </div>
        {err && <p className="err">{err}</p>}
        <button className="btn btn-primary" disabled={busy}>{busy ? "Creating…" : "Create account"}</button>
      </form>
    </AuthWrap>
  );
}
