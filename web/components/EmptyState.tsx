import Link from "next/link";

// EmptyState always offers a next action. The cta is either a link (href) or an
// in-page action (onClick) — the latter for same-page state resets (e.g.
// clearing filters) where a Link to the current route would be an inert no-op.
export function EmptyState({
  title,
  body,
  cta,
}: {
  title: string;
  body?: string;
  cta?: { label: string; href?: string; onClick?: () => void };
}) {
  return (
    <div className="card" style={{ textAlign: "center", padding: "2rem 1rem" }}>
      <h3 style={{ margin: "0 0 0.4rem" }}>{title}</h3>
      {body && <p className="muted" style={{ margin: "0 0 1rem", maxWidth: 420, marginInline: "auto" }}>{body}</p>}
      {cta &&
        (cta.onClick ? (
          <button type="button" className="btn" onClick={cta.onClick} style={{ display: "inline-block" }}>
            {cta.label}
          </button>
        ) : cta.href ? (
          <Link href={cta.href} className="btn" style={{ display: "inline-block" }}>
            {cta.label}
          </Link>
        ) : null)}
    </div>
  );
}
