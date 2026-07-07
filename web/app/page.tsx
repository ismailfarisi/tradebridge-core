"use client";

import { useToken } from "@/lib/auth";

export default function Landing() {
  const token = useToken();
  return (
    <main
      style={{
        minHeight: "100vh",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        gap: "1.5rem",
        padding: "2rem",
        textAlign: "center",
      }}
    >
      <div>
        <h1 style={{ fontSize: "2.4rem" }}>TradeBridge</h1>
        <p className="muted" style={{ fontSize: "1.05rem", marginTop: "0.5rem", maxWidth: 520 }}>
          A neutral B2B RFQ marketplace. Buyers post requests in plain language; seller agents
          reason over their catalogs and quote automatically.
        </p>
      </div>
      <div className="row">
        {token ? (
          <a className="btn btn-primary" href="/dashboard">
            Go to dashboard →
          </a>
        ) : (
          <>
            <a className="btn btn-primary" href="/register">
              Create account
            </a>
            <a className="btn" href="/login">
              Sign in
            </a>
          </>
        )}
      </div>
    </main>
  );
}
