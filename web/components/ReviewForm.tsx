"use client";

import { useState } from "react";
import { StarRating } from "@/components/StarRating";

// ReviewForm is a star-rating + free-text review submission form. The caller
// owns persistence via onSubmit; this component only owns local form state
// and success/error messaging.
export function ReviewForm({ onSubmit }: { onSubmit: (rating: number, body: string) => Promise<void> }) {
  const [rating, setRating] = useState(5);
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    setMsg(null);
    try {
      await onSubmit(rating, body);
      setMsg("Review submitted.");
      setBody("");
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "review failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="col" onSubmit={submit} style={{ gap: "0.6rem" }}>
      <div className="field">
        <label>Rating</label>
        <StarRating value={rating} onChange={setRating} />
      </div>
      <div className="field">
        <label>Review</label>
        <textarea className="input" rows={3} value={body} onChange={(e) => setBody(e.target.value)} placeholder="How did it go?" />
      </div>
      <div>
        <button className="btn btn-primary btn-sm" disabled={busy}>{busy ? "Submitting…" : "Submit review"}</button>
      </div>
      {msg && <p className="ok">{msg}</p>}
      {err && <p className="err">{err}</p>}
    </form>
  );
}
