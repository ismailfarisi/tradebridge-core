"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useToken } from "@/lib/auth";
import { getActivation, type Activation } from "@/lib/activation";

export function ActivationChecklist() {
  const token = useToken();
  const [act, setAct] = useState<Activation | null>(null);

  useEffect(() => {
    if (!token) return;
    getActivation(token).then(setAct).catch(() => {});
  }, [token]);

  if (!act || act.percent_complete >= 100) return null;

  return (
    <div className="card" style={{ marginBottom: "1.5rem" }}>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0, fontSize: "1.1rem" }}>Get started</h2>
        <span className="muted small">{act.percent_complete}% complete</span>
      </div>
      <div role="progressbar" aria-valuenow={act.percent_complete} aria-valuemin={0} aria-valuemax={100}
        style={{ height: 6, borderRadius: 3, background: "var(--border, #e5e7eb)", margin: "0.6rem 0 1rem", overflow: "hidden" }}>
        <div style={{ width: `${act.percent_complete}%`, height: "100%", background: "var(--primary)" }} />
      </div>
      <ul className="col" style={{ listStyle: "none", padding: 0, margin: 0, gap: "0.5rem" }}>
        {act.steps.map((s) => (
          <li key={s.key} className="row" style={{ justifyContent: "space-between", alignItems: "center", gap: "0.75rem" }}>
            <span className="row" style={{ gap: "0.5rem", alignItems: "center" }}>
              <span aria-hidden style={{ color: s.done ? "var(--success, #22c55e)" : "var(--muted, #9ca3af)" }}>{s.done ? "✓" : "○"}</span>
              <span style={{ textDecoration: s.done ? "line-through" : "none" }}>
                {s.label}
                <span className="muted small" style={{ display: "block" }}>{s.hint}</span>
              </span>
            </span>
            {!s.done && <Link href={s.cta_href} className="btn-sm">Do it</Link>}
          </li>
        ))}
      </ul>
    </div>
  );
}
