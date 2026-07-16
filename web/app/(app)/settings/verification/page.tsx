"use client";

import { useEffect, useState } from "react";
import { PageHeader } from "@/components/AppShell";
import { VerifiedBadge } from "@/components/VerifiedBadge";
import { useToken } from "@/lib/auth";
import { getVerification, claimDomain, checkDomain, type VerificationState, type Challenge } from "@/lib/trust";

export default function VerificationPage() {
  const token = useToken();
  const [state, setState] = useState<VerificationState | null>(null);
  const [domain, setDomain] = useState("");
  const [challenge, setChallenge] = useState<Challenge | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const refresh = () => {
    if (!token) return;
    getVerification(token).then(setState).catch(() => {});
  };
  useEffect(refresh, [token]);
  useEffect(() => {
    // Prefill the domain input from an existing pending claim.
    if (state?.pending?.length) {
      setDomain(state.pending[0].domain);
      setChallenge(state.pending[0]);
    }
  }, [state]);

  const claim = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true); setErr(null); setMsg(null);
    try {
      const ch = await claimDomain(token, domain);
      setChallenge(ch);
      setMsg("Add the DNS record below, then click Verify now. DNS can take a few minutes to propagate.");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "claim failed");
    } finally { setBusy(false); }
  };

  const verify = async () => {
    if (!token || !challenge) return;
    setBusy(true); setErr(null); setMsg(null);
    try {
      const r = await checkDomain(token, challenge.domain);
      if (r.verified) {
        setMsg("Verified! Your storefront and quotes now show the verified badge.");
        refresh();
      } else {
        setMsg("Not found yet — the TXT record isn't visible. Give DNS a few minutes and try again.");
      }
    } catch (e) {
      setErr(e instanceof Error ? e.message : "check failed");
    } finally { setBusy(false); }
  };

  const verified = state?.verification_status === "verified";

  return (
    <>
      <PageHeader title="Verification" subtitle="Prove you control your domain to earn the verified badge." />
      <div className="card" style={{ maxWidth: 640 }}>
        <div className="row" style={{ gap: "0.5rem", alignItems: "center", marginBottom: "0.6rem" }}>
          <strong>Status:</strong>
          <VerifiedBadge status={state?.verification_status} />
          <span className="muted">{state?.verification_status ?? "…"}</span>
        </div>
        {verified ? (
          <p className="ok">Verified domain: <strong>{state?.verified_domain}</strong></p>
        ) : (
          <form className="col" onSubmit={claim}>
            <div className="field">
              <label>Domain</label>
              <input className="input" value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="acme.com" />
            </div>
            <div><button className="btn btn-primary" disabled={busy || !domain}>{busy ? "Working…" : "Get DNS record"}</button></div>
          </form>
        )}
        {challenge && !verified && (
          <div style={{ marginTop: "1rem" }}>
            <p className="muted">Add this TXT record at your DNS provider:</p>
            <table className="tbl">
              <tbody>
                <tr><td><strong>Type</strong></td><td>{challenge.record_type}</td></tr>
                <tr><td><strong>Name</strong></td><td><code>{challenge.record_name}</code></td></tr>
                <tr><td><strong>Value</strong></td><td><code>{challenge.record_value}</code></td></tr>
              </tbody>
            </table>
            <div style={{ marginTop: "0.7rem" }}>
              <button className="btn btn-primary" onClick={verify} disabled={busy}>{busy ? "Checking…" : "Verify now"}</button>
            </div>
          </div>
        )}
        {msg && <p className="ok">{msg}</p>}
        {err && <p className="err">{err}</p>}
      </div>
    </>
  );
}
