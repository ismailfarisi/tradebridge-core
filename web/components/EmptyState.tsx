import Link from "next/link";

export function EmptyState({
  title,
  body,
  cta,
}: {
  title: string;
  body?: string;
  cta?: { label: string; href: string };
}) {
  return (
    <div className="card" style={{ textAlign: "center", padding: "2rem 1rem" }}>
      <h3 style={{ margin: "0 0 0.4rem" }}>{title}</h3>
      {body && <p className="muted" style={{ margin: "0 0 1rem", maxWidth: 420, marginInline: "auto" }}>{body}</p>}
      {cta && (
        <Link href={cta.href} className="btn" style={{ display: "inline-block" }}>
          {cta.label}
        </Link>
      )}
    </div>
  );
}
