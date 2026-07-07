"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { register, setToken, type CompanyType } from "@/lib/api";
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
      router.replace("/dashboard");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "registration failed");
      setBusy(false);
    }
  };

  return (
    <AuthWrap title="Create account" alt={<>Already registered? <a href="/login">Sign in</a></>}>
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
