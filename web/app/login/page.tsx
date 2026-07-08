"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { login, setToken } from "@/lib/api";

export default function LoginPage() {
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
      router.replace("/dashboard");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "login failed");
      setBusy(false);
    }
  };

  return (
    <AuthWrap title="Sign in" alt={<>No account? <Link href="/register">Create one</Link></>}>
      <form className="col" onSubmit={submit}>
        <div className="field">
          <label>Email</label>
          <input className="input" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </div>
        <div className="field">
          <label>Password</label>
          <input className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </div>
        {err && <p className="err">{err}</p>}
        <button className="btn btn-primary" disabled={busy}>{busy ? "Signing in…" : "Sign in"}</button>
      </form>
    </AuthWrap>
  );
}

export function AuthWrap({ title, children, alt }: { title: string; children: React.ReactNode; alt: React.ReactNode }) {
  return (
    <main style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center", padding: "2rem" }}>
      <div className="card" style={{ width: "100%", maxWidth: 400 }}>
        <Link href="/" style={{ color: "var(--text)", fontWeight: 700, fontSize: "1.15rem" }}>TradeBridge</Link>
        <h1 style={{ fontSize: "1.3rem", margin: "1rem 0 1.25rem" }}>{title}</h1>
        {children}
        <p className="muted small" style={{ marginTop: "1rem", marginBottom: 0 }}>{alt}</p>
      </div>
    </main>
  );
}
