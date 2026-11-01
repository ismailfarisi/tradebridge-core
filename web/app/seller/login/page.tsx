"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { login, me, setToken } from "@/lib/api";
import { sellerLandingPath } from "@/lib/seller";
import { AuthWrap } from "../../login/page";

// Supplier sign-in. Same credentials as the main sign-in; sellers land on
// their RFQ inbox instead of the dashboard.
export default function SellerLoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const { token } = await login(email, password);
      setToken(token);
      const profile = await me(token);
      router.replace(sellerLandingPath(profile));
    } catch (e) {
      setErr(e instanceof Error ? e.message : "login failed");
      setBusy(false);
    }
  };

  return (
    <AuthWrap
      title="Supplier sign in"
      alt={
        <>
          New supplier? <Link href="/seller/register">Create a seller account</Link>
          <br />
          Buying instead? <Link href="/login">Buyer sign in</Link>
        </>
      }
    >
      <p className="muted small" style={{ marginTop: "-0.5rem" }}>
        See the RFQs matched to your catalog and track your quotes.
      </p>
      <form className="col" onSubmit={submit}>
        <div className="field">
          <label htmlFor="seller-email">Work email</label>
          <input id="seller-email" className="input" type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </div>
        <div className="field">
          <label htmlFor="seller-password">Password</label>
          <input id="seller-password" className="input" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </div>
        {err && <p className="err">{err}</p>}
        <button className="btn btn-primary" disabled={busy}>{busy ? "Signing in…" : "Sign in"}</button>
      </form>
    </AuthWrap>
  );
}
