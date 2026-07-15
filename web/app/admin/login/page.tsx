"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { adminLogin, setAdminToken } from "@/lib/admin";

export default function AdminLogin() {
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
      const { token } = await adminLogin(email, password);
      setAdminToken(token);
      router.replace("/admin");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "login failed");
      setBusy(false);
    }
  };

  return (
    <main style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center", padding: "2rem" }}>
      <div className="card" style={{ width: "100%", maxWidth: 400 }}>
        <strong style={{ fontSize: "1.15rem" }}>TradeBridge · Platform Admin</strong>
        <h1 style={{ fontSize: "1.3rem", margin: "1rem 0 1.25rem" }}>Admin sign in</h1>
        <form className="col" onSubmit={submit}>
          <div className="field"><label htmlFor="admin-email">Email</label><input id="admin-email" name="email" className="input" type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required /></div>
          <div className="field"><label htmlFor="admin-password">Password</label><input id="admin-password" name="password" className="input" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required /></div>
          {err && <p className="err">{err}</p>}
          <button className="btn btn-primary" disabled={busy}>{busy ? "Signing in…" : "Sign in"}</button>
        </form>
      </div>
    </main>
  );
}
