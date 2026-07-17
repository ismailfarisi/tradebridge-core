"use client";
import { useCallback, useEffect, useState } from "react";
import { acceptQuote } from "@/lib/api";
import { getThread, postCounter, streamThread, type Thread } from "@/lib/thread";

// The post-quote negotiation thread: message history + version history
// (seller revisions and buyer counters) + a counter-offer form + accept the
// latest version. Distinct from components/QuoteThread.tsx, which is the
// pre-acceptance buyer<->seller-agent clarification chat on the RFQ chat
// page — this one lives on the compare page and understands quote versions.
export function NegotiationThread({
  quoteId,
  token,
  onAccepted,
}: {
  quoteId: string;
  token: string;
  onAccepted?: (orderId: string) => void;
}) {
  const [thread, setThread] = useState<Thread | null>(null);
  const [loadErr, setLoadErr] = useState<string | null>(null);

  const [price, setPrice] = useState("");
  const [delivery, setDelivery] = useState("");
  const [message, setMessage] = useState("");
  const [sending, setSending] = useState(false);
  const [counterErr, setCounterErr] = useState<string | null>(null);

  const [accepting, setAccepting] = useState(false);
  const [acceptErr, setAcceptErr] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const t = await getThread(token, quoteId);
      setThread(t);
      setLoadErr(null);
    } catch (e) {
      setLoadErr(e instanceof Error ? e.message : "Couldn't load thread");
    }
  }, [token, quoteId]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    refresh();
    const ctrl = streamThread(token, quoteId, () => {
      refresh();
    });
    return () => ctrl.abort();
  }, [refresh, token, quoteId]);

  const submitCounter = async (e: React.FormEvent) => {
    e.preventDefault();
    const body = message.trim();
    if (!body || sending) return;
    setSending(true);
    setCounterErr(null);
    try {
      await postCounter(token, quoteId, {
        ...(price.trim() ? { price_cents: Math.round(parseFloat(price) * 100) } : {}),
        ...(delivery.trim() ? { delivery_days: parseInt(delivery, 10) } : {}),
        message: body,
      });
      setPrice("");
      setDelivery("");
      setMessage("");
      await refresh();
    } catch (err) {
      setCounterErr(err instanceof Error ? err.message : "Couldn't send counter — retry");
    } finally {
      setSending(false);
    }
  };

  const acceptLatest = async () => {
    setAccepting(true);
    setAcceptErr(null);
    try {
      const { order_id } = await acceptQuote(token, quoteId);
      onAccepted?.(order_id);
      await refresh();
    } catch (err) {
      setAcceptErr(err instanceof Error ? err.message : "Accept failed");
    } finally {
      setAccepting(false);
    }
  };

  if (loadErr) return <p className="err">{loadErr}</p>;
  if (!thread) return <p className="muted small">Loading thread…</p>;

  const versions = [...thread.versions].sort((a, b) => b.version - a.version);
  const latest = versions[0];

  return (
    <div className="col" style={{ gap: "0.85rem", marginTop: "0.6rem" }}>
      {versions.length > 0 && (
        <table className="tbl">
          <thead>
            <tr>
              <th>Version</th>
              <th>Price</th>
              <th>Delivery</th>
              <th>Source</th>
              <th>Date</th>
            </tr>
          </thead>
          <tbody>
            {versions.map((v) => (
              <tr key={v.id} style={v.version === latest.version ? { fontWeight: 650 } : undefined}>
                <td>v{v.version}{v.version === latest.version ? " (latest)" : ""}</td>
                <td>{v.currency} {(v.price_cents / 100).toFixed(2)}</td>
                <td className="muted">{v.delivery_days != null ? `${v.delivery_days} days` : "—"}</td>
                <td><span className="badge">{v.source}</span></td>
                <td className="muted small">{new Date(v.created_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <div className="col" style={{ gap: "0.5rem" }}>
        {thread.messages.map((m) => (
          <div
            key={m.id}
            className="card"
            style={{
              padding: "0.5rem 0.75rem",
              alignSelf: m.author === "buyer" ? "flex-end" : "flex-start",
              maxWidth: "85%",
            }}
          >
            <div className="muted" style={{ fontSize: "0.75rem" }}>{m.author === "buyer" ? "You" : "Seller"}</div>
            <div>{m.body}</div>
          </div>
        ))}
        {thread.messages.length === 0 && <div className="muted small">No messages yet.</div>}
      </div>

      {thread.quote.status !== "accepted" && (
        <form onSubmit={submitCounter} className="col" style={{ gap: "0.5rem" }}>
          <div className="row wrap" style={{ gap: "0.5rem" }}>
            <div className="field" style={{ flex: "1 1 120px" }}>
              <label htmlFor={`counter-price-${quoteId}`}>Price (optional)</label>
              <input
                id={`counter-price-${quoteId}`}
                className="input"
                type="number"
                step="0.01"
                min="0"
                placeholder={latest ? (latest.price_cents / 100).toFixed(2) : "0.00"}
                value={price}
                onChange={(e) => setPrice(e.target.value)}
              />
            </div>
            <div className="field" style={{ flex: "1 1 120px" }}>
              <label htmlFor={`counter-delivery-${quoteId}`}>Delivery days (optional)</label>
              <input
                id={`counter-delivery-${quoteId}`}
                className="input"
                type="number"
                min="0"
                placeholder={latest?.delivery_days != null ? String(latest.delivery_days) : "—"}
                value={delivery}
                onChange={(e) => setDelivery(e.target.value)}
              />
            </div>
          </div>
          <div className="field">
            <label htmlFor={`counter-message-${quoteId}`}>Message</label>
            <input
              id={`counter-message-${quoteId}`}
              className="input"
              required
              placeholder="Explain your counter-offer…"
              value={message}
              onChange={(e) => setMessage(e.target.value)}
            />
          </div>
          {counterErr && <div className="err">{counterErr}</div>}
          <div className="row" style={{ gap: "0.5rem" }}>
            <button className="btn btn-sm" disabled={sending || !message.trim()}>
              {sending ? "Sending…" : "Send counter"}
            </button>
            {latest && (
              <button
                type="button"
                className="btn btn-sm btn-primary"
                disabled={accepting}
                onClick={acceptLatest}
              >
                {accepting ? "…" : `Accept latest (v${latest.version})`}
              </button>
            )}
          </div>
          {acceptErr && <div className="err">{acceptErr}</div>}
        </form>
      )}
    </div>
  );
}
