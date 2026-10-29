import type { ReactNode } from "react";

export default function LegalShell({
  title,
  updated,
  children,
}: {
  title: string;
  updated: string;
  children: ReactNode;
}) {
  return (
    <main style={{ maxWidth: 760, margin: "0 auto", padding: "0 1rem 3rem" }}>
      <div
        role="note"
        className="small"
        style={{
          background: "var(--warning-soft)",
          border: "1px solid #fedf89",
          color: "var(--warning)",
          borderRadius: "var(--radius)",
          padding: "0.75rem 1rem",
          margin: "1.5rem 0",
          fontWeight: 650,
        }}
      >
        DRAFT — not legal advice, pending legal review before launch.
      </div>
      <h1 style={{ fontSize: "1.6rem", marginBottom: "0.25rem" }}>{title}</h1>
      <p className="small muted" style={{ marginTop: 0 }}>
        Last updated: {updated}
      </p>
      <div className="col" style={{ gap: "1.25rem", lineHeight: 1.7 }}>
        {children}
      </div>
    </main>
  );
}
