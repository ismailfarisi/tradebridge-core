"use client";

import { useEffect, useState } from "react";
import { PageHeader } from "@/components/AppShell";
import UpgradeModal from "@/components/UpgradeModal";
import UsageMeter from "@/components/UsageMeter";
import { useToken } from "@/lib/auth";
import {
  getBillingSummary, inviteMember, listMembers, startCheckout,
  PaywallError, type BillingSummary, type Member,
} from "@/lib/billing";

export default function TeamPage() {
  const token = useToken();
  const [members, setMembers] = useState<Member[]>([]);
  const [summary, setSummary] = useState<BillingSummary | null>(null);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<"admin" | "member">("member");
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [paywall, setPaywall] = useState<PaywallError | null>(null);
  const [upgrading, setUpgrading] = useState(false);

  const refresh = () => {
    if (!token) return;
    listMembers(token).then(setMembers).catch((e) => setErr(e.message));
    getBillingSummary(token).then(setSummary).catch(() => {});
  };

  useEffect(refresh, [token]);

  const invite = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true);
    setErr(null);
    setOk(null);
    try {
      await inviteMember(token, email, role);
      setOk(`Invited ${email}.`);
      setEmail("");
      refresh();
    } catch (e) {
      if (e instanceof PaywallError) setPaywall(e);
      else setErr(e instanceof Error ? e.message : "invite failed");
    } finally {
      setBusy(false);
    }
  };

  const upgrade = async () => {
    if (!token || !paywall) return;
    const plan = paywall.planRequired === "business" ? "business" : "pro";
    setUpgrading(true);
    try {
      window.location.href = await startCheckout(token, plan, "month");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "checkout failed");
      setUpgrading(false);
      setPaywall(null);
    }
  };

  return (
    <>
      <PageHeader title="Team" subtitle="Invite teammates. Seats are limited by your plan." />
      {summary && (
        <div className="card" style={{ marginBottom: "1.25rem" }}>
          <UsageMeter label="Seats used" used={summary.usage.seats_used} limit={summary.usage.seats_limit} />
        </div>
      )}

      <form onSubmit={invite} className="card" style={{ marginBottom: "1.25rem" }}>
        <h2 style={{ fontSize: "1.05rem", marginBottom: "0.75rem" }}>Invite a teammate</h2>
        <div className="row" style={{ gap: "0.6rem", flexWrap: "wrap" }}>
          <input className="input" style={{ flex: 1, minWidth: 200 }} type="email" required placeholder="name@company.com"
            value={email} onChange={(e) => setEmail(e.target.value)} />
          <select className="select" value={role} onChange={(e) => setRole(e.target.value as "admin" | "member")}>
            <option value="member">Member</option>
            <option value="admin">Admin</option>
          </select>
          <button className="btn btn-primary" disabled={busy || !email}>{busy ? "…" : "Send invite"}</button>
        </div>
        {err && <div className="notice err" style={{ marginTop: "0.75rem" }}>{err}</div>}
        {ok && <div className="notice" style={{ marginTop: "0.75rem" }}>{ok}</div>}
      </form>

      <div className="card">
        <h2 style={{ fontSize: "1.05rem", marginBottom: "0.75rem" }}>Members</h2>
        <table className="tbl" style={{ width: "100%" }}>
          <thead><tr><th style={{ textAlign: "left" }}>Email</th><th style={{ textAlign: "left" }}>Role</th></tr></thead>
          <tbody>
            {members.map((m) => (
              <tr key={m.id}><td>{m.email}</td><td><span className="badge">{m.role}</span></td></tr>
            ))}
          </tbody>
        </table>
      </div>

      {paywall && (
        <UpgradeModal
          feature={paywall.feature}
          planRequired={paywall.planRequired}
          onClose={() => setPaywall(null)}
          onUpgrade={upgrade}
          busy={upgrading}
        />
      )}
    </>
  );
}
