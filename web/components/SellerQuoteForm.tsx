"use client";

import { useState } from "react";
import {
  QUOTE_CURRENCIES,
  parseDeliveryDays,
  parsePrice,
  submitSellerQuote,
  type SellerQuote,
  type SellerQuoteResult,
} from "@/lib/seller";
import { PAYMENT_TERMS_OPTIONS, paymentTermsLabel } from "@/lib/terms";

// SellerQuoteForm lets a seller quote an RFQ by hand. With an existing quote
// it is prefilled and sends a revised offer instead (the buyer sees a new
// version and a thread message).
export function SellerQuoteForm({
  token,
  rfqId,
  existing,
  onSaved,
  onCancel,
}: {
  token: string;
  rfqId: string;
  existing?: SellerQuote | null;
  onSaved: (r: SellerQuoteResult) => void;
  onCancel?: () => void;
}) {
  const [price, setPrice] = useState(existing ? String(existing.price_cents / 100) : "");
  const [currency, setCurrency] = useState(existing?.currency || "AED");
  const [delivery, setDelivery] = useState(existing?.delivery_days != null ? String(existing.delivery_days) : "");
  const [terms, setTerms] = useState(existing?.payment_terms_days != null ? String(existing.payment_terms_days) : "");
  const [notes, setNotes] = useState(existing?.notes ?? "");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const revising = Boolean(existing);
  const currencies: string[] = QUOTE_CURRENCIES.includes(currency as (typeof QUOTE_CURRENCIES)[number])
    ? [...QUOTE_CURRENCIES]
    : [currency, ...QUOTE_CURRENCIES];

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const amount = parsePrice(price);
    if (amount === null) {
      setErr("Enter a total price greater than zero, with at most two decimals.");
      return;
    }
    const days = parseDeliveryDays(delivery);
    if (days === undefined) {
      setErr("Delivery time must be a whole number of days.");
      return;
    }
    setBusy(true);
    setErr(null);
    try {
      const res = await submitSellerQuote(token, rfqId, {
        price: amount,
        currency,
        delivery_days: days,
        ...(terms !== "" ? { payment_terms_days: Number(terms) } : {}),
        notes: notes.trim(),
      });
      onSaved(res);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Couldn't send the quote. Try again.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="col" onSubmit={submit} aria-label={revising ? "Revise your quote" : "Send a quote"}>
      <div className="field">
        <label htmlFor="sq-price">Total price</label>
        <div className="row" style={{ gap: "0.5rem" }}>
          <select
            className="input"
            aria-label="Currency"
            value={currency}
            onChange={(e) => setCurrency(e.target.value)}
            style={{ width: "auto", flex: "0 0 auto" }}
          >
            {currencies.map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
          <input
            id="sq-price"
            className="input"
            inputMode="decimal"
            placeholder="e.g. 12,500"
            value={price}
            onChange={(e) => setPrice(e.target.value)}
            required
            style={{ flex: 1, minWidth: 0 }}
          />
        </div>
        <span className="muted small">For the full quantity requested, delivered.</span>
      </div>
      <div className="field">
        <label htmlFor="sq-days">Delivery time in days <span className="muted small">(optional)</span></label>
        <input id="sq-days" className="input" inputMode="numeric" placeholder="e.g. 7" value={delivery} onChange={(e) => setDelivery(e.target.value)} />
      </div>
      <div className="field">
        <label htmlFor="sq-terms">Payment terms <span className="muted small">(optional)</span></label>
        <select id="sq-terms" className="input" value={terms} onChange={(e) => setTerms(e.target.value)}>
          {(!revising || existing?.payment_terms_days == null) && <option value="">Not stated</option>}
          {PAYMENT_TERMS_OPTIONS.map((d) => <option key={d} value={String(d)}>{paymentTermsLabel(d)}</option>)}
        </select>
        <span className="muted small">When the buyer pays after delivery. Buyers may offer a better price for shorter terms.</span>
      </div>
      <div className="field">
        <label htmlFor="sq-notes">Notes for the buyer <span className="muted small">(optional)</span></label>
        <textarea
          id="sq-notes"
          className="input"
          rows={3}
          maxLength={2000}
          placeholder="Specs, packaging, what's included…"
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
        />
      </div>
      {err && <p className="err" role="alert">{err}</p>}
      <div className="row wrap" style={{ gap: "0.5rem" }}>
        <button className="btn btn-primary" disabled={busy}>
          {busy ? "Sending…" : revising ? "Send revised offer" : "Send quote"}
        </button>
        {onCancel && (
          <button type="button" className="btn" onClick={onCancel} disabled={busy}>Cancel</button>
        )}
      </div>
    </form>
  );
}
