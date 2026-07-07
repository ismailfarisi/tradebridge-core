"use client";

import { useEffect, useState } from "react";
import { PageHeader } from "@/components/AppShell";
import { useMe } from "@/lib/auth";
import { listCatalog, listRfqs } from "@/lib/api";

export default function Dashboard() {
  const { token, me } = useMe();
  const [rfqCount, setRfqCount] = useState<number | null>(null);
  const [catCount, setCatCount] = useState<number | null>(null);

  useEffect(() => {
    if (!token || !me) return;
    const buyer = me.company_type !== "seller";
    const seller = me.company_type === "seller" || me.company_type === "both";
    if (buyer) listRfqs(token).then((r) => setRfqCount(r.count)).catch(() => {});
    if (seller) listCatalog(token).then((r) => setCatCount(r.count)).catch(() => {});
  }, [token, me]);

  if (!me) return null;
  const seller = me.company_type === "seller" || me.company_type === "both";
  const buyer = me.company_type !== "seller";
  const admin = me.role === "owner" || me.role === "admin";

  return (
    <>
      <PageHeader title={`Welcome, ${me.company_name ?? "there"}`} subtitle="Your marketplace at a glance." />

      <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))", marginBottom: "1.5rem" }}>
        {buyer && <Stat n={rfqCount} label="Your RFQs" />}
        {seller && <Stat n={catCount} label="Catalog items" />}
        <div className="stat">
          <div className="n" style={{ textTransform: "capitalize" }}>{me.company_type ?? "—"}</div>
          <div className="l">Account type</div>
        </div>
      </div>

      <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(230px,1fr))" }}>
        {buyer && <Action href="/rfqs" title="Post an RFQ" body="Describe what you need in plain language and get ranked quotes." />}
        {seller && <Action href="/seller/catalog" title="Manage catalog" body="Upload products so your agent can quote accurately." />}
        {seller && <Action href="/seller/agent" title="Agent credentials" body="Issue a credential so your seller agent can connect." />}
        {admin && <Action href="/admin/models" title="Model settings" body="Choose the provider + model that powers reasoning." />}
        {admin && <Action href="/admin/usage" title="Usage & billing" body="Track requests, tokens and spend across the platform." />}
        {admin && <Action href="/admin/fraud" title="Fraud detection" body="Review flagged activity and risk signals." />}
      </div>
    </>
  );
}

function Stat({ n, label }: { n: number | null; label: string }) {
  return (
    <div className="stat">
      <div className="n">{n ?? "—"}</div>
      <div className="l">{label}</div>
    </div>
  );
}

function Action({ href, title, body }: { href: string; title: string; body: string }) {
  return (
    <a href={href} className="card" style={{ display: "block" }}>
      <strong style={{ color: "var(--text)" }}>{title}</strong>
      <p className="muted small" style={{ margin: "0.35rem 0 0" }}>{body}</p>
    </a>
  );
}
