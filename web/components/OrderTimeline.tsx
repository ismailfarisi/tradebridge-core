import { orderStatus, type OrderEvent } from "@/lib/orders";
import { when } from "@/lib/negotiation";

// OrderTimeline renders an ordered list of order lifecycle transitions,
// oldest first (the API already returns them in creation order).
export function OrderTimeline({ events }: { events: OrderEvent[] }) {
  if (events.length === 0) return <p className="muted">No events yet.</p>;
  return (
    <ol className="col" style={{ gap: "0.5rem", listStyle: "none", padding: 0, margin: 0 }}>
      {events.map((e) => (
        <li key={e.id} style={{ borderTop: "1px solid var(--border)", paddingTop: "0.5rem" }}>
          <div className="row" style={{ gap: "0.4rem", alignItems: "center" }}>
            <strong className="small">
              {e.from_status ? `${orderStatus(e.from_status).label} → ` : ""}{orderStatus(e.to_status).label}
            </strong>
            <span className="muted small">
              by {e.actor} · {when(e.created_at)}
            </span>
          </div>
          {e.note && <p className="muted small" style={{ margin: "0.2rem 0 0" }}>{e.note}</p>}
        </li>
      ))}
    </ol>
  );
}
