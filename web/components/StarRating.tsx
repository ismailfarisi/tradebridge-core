"use client";

// StarRating is presentational: read-only when `onChange` is omitted (shows
// filled/empty stars + an optional review count), or a 5-star clickable input
// when `onChange` is given.
export function StarRating({
  value,
  count,
  onChange,
}: {
  value: number;
  count?: number;
  onChange?: (n: number) => void;
}) {
  const rounded = Math.round(value);
  const stars = [1, 2, 3, 4, 5];

  if (!onChange) {
    return (
      <span className="row small" style={{ gap: "0.3rem", display: "inline-flex", alignItems: "center" }}>
        <span aria-label={`${value.toFixed(1)} out of 5 stars`}>
          {stars.map((n) => (n <= rounded ? "★" : "☆")).join("")}
        </span>
        {count != null && <span className="muted">({count})</span>}
      </span>
    );
  }

  return (
    <span className="row" style={{ gap: "0.15rem", display: "inline-flex" }}>
      {stars.map((n) => (
        <button
          key={n}
          type="button"
          className="btn-ghost"
          aria-label={`${n} star${n === 1 ? "" : "s"}`}
          onClick={() => onChange(n)}
          style={{ border: "none", background: "none", cursor: "pointer", fontSize: "1.2rem", padding: "0 0.05rem", lineHeight: 1 }}
        >
          {n <= value ? "★" : "☆"}
        </button>
      ))}
    </span>
  );
}
