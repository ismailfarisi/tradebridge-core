"use client";

import { useEffect, useState } from "react";
import { fetchHealth, CORE_URL, type HealthState } from "@/lib/api";

type Status = "loading" | HealthState;

export default function Home() {
  const [status, setStatus] = useState<Status>("loading");

  useEffect(() => {
    let active = true;
    const check = async () => {
      const s = await fetchHealth();
      if (active) setStatus(s);
    };
    check();
    const id = setInterval(check, 5000);
    return () => {
      active = false;
      clearInterval(id);
    };
  }, []);

  const color =
    status === "ok" ? "#16a34a" : status === "loading" ? "#a1a1aa" : "#dc2626";
  const label =
    status === "ok"
      ? "ok"
      : status === "loading"
        ? "checking…"
        : "unreachable";

  return (
    <main
      style={{
        minHeight: "100vh",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        fontFamily: "system-ui, sans-serif",
        gap: "1.25rem",
        padding: "2rem",
      }}
    >
      <h1 style={{ fontSize: "1.75rem", fontWeight: 600 }}>
        TradeBridge — Buyer / Admin
      </h1>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: "0.6rem",
          fontSize: "1.05rem",
        }}
      >
        <span
          aria-hidden
          style={{
            width: 12,
            height: 12,
            borderRadius: "50%",
            background: color,
            display: "inline-block",
          }}
        />
        <span>
          Core backend: <strong>{label}</strong>
        </span>
      </div>
      <code style={{ color: "#71717a", fontSize: "0.85rem" }}>{CORE_URL}</code>
      <a href="/settings" style={{ color: "#2563eb", fontSize: "0.95rem", fontWeight: 600 }}>
        Provider settings →
      </a>
    </main>
  );
}
