"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import Link from "next/link";
import { verifyEmail } from "@/lib/notifications";

function VerifyContent() {
  const params = useSearchParams();
  const token = params.get("token");
  const [state, setState] = useState<"pending" | "ok" | "error">("pending");

  useEffect(() => {
    if (!token) { setState("error"); return; }
    let active = true;
    verifyEmail(token)
      .then(() => active && setState("ok"))
      .catch(() => active && setState("error"));
    return () => { active = false; };
  }, [token]);

  return (
    <div className="card" style={{ maxWidth: 420, textAlign: "center" }}>
      {state === "pending" && <p className="muted">Verifying your email…</p>}
      {state === "ok" && (
        <>
          <h2>Email verified ✓</h2>
          <p className="muted">Your account is now fully active.</p>
          <Link className="btn btn-primary" href="/dashboard">Go to dashboard</Link>
        </>
      )}
      {state === "error" && (
        <>
          <h2>Verification failed</h2>
          <p className="muted">This link is invalid or has expired. Sign in and resend the verification email from the banner.</p>
          <Link className="btn" href="/login">Sign in</Link>
        </>
      )}
    </div>
  );
}

export default function VerifyPage() {
  return (
    <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center", padding: "2rem" }}>
      <Suspense fallback={<div className="card" style={{ maxWidth: 420, textAlign: "center" }}><p className="muted">Verifying your email…</p></div>}>
        <VerifyContent />
      </Suspense>
    </div>
  );
}
