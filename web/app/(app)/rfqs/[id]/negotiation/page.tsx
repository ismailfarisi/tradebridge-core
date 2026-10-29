"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { PageHeader } from "@/components/AppShell";
import { Icon } from "@/components/Icon";
import { useToken } from "@/lib/auth";
import {
  DEFAULT_CURRENCY,
  approveNegotiation,
  dollarsToCents,
  getNegotiation,
  money,
  pauseNegotiation,
  pct,
  resumeNegotiation,
  runNegotiation,
  saveStrategy,
  STATUS_LABEL,
  statusClass,
  type NegotiationView,
  type Round,
  type Tone,
} from "@/lib/negotiation";

const LIVE_POLL_MS = 5000;

export default function NegotiationRoom() {
  const token = useToken();
  const { id } = useParams<{ id: string }>();
  const [view, setView] = useState<NegotiationView | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [orderId, setOrderId] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);

  const load = useCallback(() => {
    if (!token || !id) return;
    getNegotiation(token, id).then(setView).catch((e) => setErr(e.message));
  }, [token, id]);

  useEffect(load, [load]);

  const live = view?.strategy?.status === "active";
  useEffect(() => {
    if (!live) return;
    const t = setInterval(load, LIVE_POLL_MS);
    return () => clearInterval(t);
  }, [live, load]);

  const act = async (fn: () => Promise<NegotiationView>) => {
    setBusy(true);
    setErr(null);
    try {
      setView(await fn());
      setEditing(false);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Request failed");
    } finally {
      setBusy(false);
    }
  };

  const approve = async () => {
    if (!token) return;
    setBusy(true);
    setErr(null);
    try {
      const order = await approveNegotiation(token, id);
      setOrderId(order.order_id);
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Approval failed");
    } finally {
      setBusy(false);
    }
  };

  if (!view) return err ? <p className="err">{err}</p> : <p className="muted">Loading negotiation…</p>;

  const s = view.strategy;
  const cur = view.summary.currency || DEFAULT_CURRENCY;
  const winner = view.suppliers.find((x) => x.recommended);
  const closed = view.rfq.status === "closed";
  const showForm = !s || editing;

  return (
    <>
      <div style={{ marginBottom: "0.6rem" }}>
        <Link href="/negotiations" className="small row" style={{ gap: "0.3rem", display: "inline-flex" }}>
          <Icon name="arrowLeft" size={14} /> Negotiations
        </Link>
      </div>
      <PageHeader
        title={view.rfq.title}
        subtitle={
          <span className="row wrap" style={{ gap: "0.5rem" }}>
            {s ? <span className={statusClass(s.status)}>{STATUS_LABEL[s.status]}</span> : <span className="badge">No agent yet</span>}
            {live && <span className="live">Agent live</span>}
            <span className="faint small">{view.summary.quote_count} supplier quote{view.summary.quote_count === 1 ? "" : "s"} · {view.rounds.length} agent move{view.rounds.length === 1 ? "" : "s"}</span>
            <Link href={`/rfqs/${id}`} className="small">RFQ details</Link>
          </span>
        }
        action={
          s && !closed ? (
            <div className="row">
              {s.status === "active" && (
                <>
                  <button className="btn" disabled={busy} onClick={() => token && act(() => runNegotiation(token, id))}>
                    <Icon name="refresh" size={16} /> Run now
                  </button>
                  <button className="btn" disabled={busy} onClick={() => token && act(() => pauseNegotiation(token, id))}>
                    <Icon name="pause" size={16} /> Pause
                  </button>
                </>
              )}
              {s.status === "paused" && (
                <button className="btn btn-primary" disabled={busy} onClick={() => token && act(() => resumeNegotiation(token, id))}>
                  <Icon name="play" size={16} /> Resume
                </button>
              )}
              {(s.status === "active" || s.status === "paused") && (
                <button className="btn btn-ghost" onClick={() => setEditing((v) => !v)}>{editing ? "Cancel" : "Edit mandate"}</button>
              )}
            </div>
          ) : null
        }
      />

      {err && <div className="banner" style={{ borderColor: "#fecdca", background: "var(--danger-soft)", color: "#b42318" }}><Icon name="x" /> {err}</div>}

      {orderId && (
        <div className="banner banner-ok">
          <Icon name="checkCircle" />
          <div className="grow"><strong>Deal approved.</strong> A purchase order was created.</div>
          <Link className="btn btn-sm" href={`/orders/${orderId}`}>View order</Link>
        </div>
      )}

      {s?.status === "awaiting_approval" && !orderId && (
        <div className="banner banner-warn">
          <Icon name="award" />
          <div className="grow">
            <strong>Ready for approval.</strong> <span className="small">{s.outcome_summary}</span>
          </div>
          <button className="btn btn-success" disabled={busy} onClick={approve}><Icon name="check" size={16} /> Approve &amp; create PO</button>
        </div>
      )}
      {s?.status === "completed" && !orderId && (
        <div className="banner banner-ok"><Icon name="checkCircle" /><div className="grow">{s.outcome_summary}</div></div>
      )}
      {s?.status === "cancelled" && (
        <div className="banner banner-info"><Icon name="x" /><div className="grow">{s.outcome_summary || "Negotiation ended without a deal."}</div></div>
      )}

      <div className="kpis">
        <div className="kpi">
          <div className="kpi-label"><Icon name="file" size={16} /> Best opening quote</div>
          <div className="kpi-value">{money(view.summary.initial_best_cents || null, cur)}</div>
        </div>
        <div className="kpi">
          <div className="kpi-label"><Icon name="target" size={16} /> Target / walk-away</div>
          <div className="kpi-value" style={{ fontSize: "1.3rem" }}>
            {s ? `${money(s.target_price_cents, cur)} / ${money(s.max_price_cents, cur)}` : "—"}
          </div>
        </div>
        <div className="kpi">
          <div className="kpi-label"><Icon name="handshake" size={16} /> Best current offer</div>
          <div className="kpi-value">{money(view.summary.current_best_cents || null, cur)}</div>
        </div>
        <div className="kpi kpi-accent">
          <div className="kpi-label"><Icon name="trending" size={16} /> Saved so far</div>
          <div className="kpi-value">{money(view.summary.savings_cents, cur)}</div>
          <div className="kpi-sub">{pct(view.summary.savings_pct)} vs opening</div>
        </div>
      </div>

      {showForm && token && (
        <MandateForm
          view={view}
          busy={busy}
          onSubmit={(input) => act(() => saveStrategy(token, id, input))}
        />
      )}

      <div className="split">
        <section className="card">
          <div className="section-head">
            <h2>Negotiation log</h2>
            {live && <span className="faint small">Updates every {LIVE_POLL_MS / 1000}s</span>}
          </div>
          {view.rounds.length === 0 ? (
            <div className="empty">
              <Icon name="bot" size={28} />
              <p style={{ margin: 0 }}>{s ? "The agent will respond as soon as suppliers quote." : "Set a mandate and the agent will counter every supplier."}</p>
            </div>
          ) : (
            <ol className="timeline">
              {[...view.rounds].reverse().map((r) => <RoundItem key={r.id} r={r} currency={cur} />)}
            </ol>
          )}
        </section>

        <section className="card">
          <div className="section-head"><h2>Supplier leaderboard</h2></div>
          {view.suppliers.length === 0 ? (
            <p className="muted small">Waiting for supplier quotes.</p>
          ) : (
            <div className="col" style={{ gap: "0.75rem" }}>
              {[...view.suppliers]
                .sort((a, b) => (s ? b.utility - a.utility : a.price_cents - b.price_cents))
                .map((q, i) => {
                  const drop = q.initial_price_cents - q.price_cents;
                  return (
                    <div key={q.quote_id} className="card" style={{ padding: "0.85rem 1rem", boxShadow: "none", borderColor: q.recommended ? "var(--success)" : undefined, background: q.recommended ? "var(--success-soft)" : undefined }}>
                      <div className="row" style={{ justifyContent: "space-between" }}>
                        <strong className="row" style={{ gap: "0.4rem" }}>
                          <span className="faint num">#{i + 1}</span> {q.seller_name}
                          {q.verification_status === "verified" && <span title="Verified" style={{ color: "var(--primary)", display: "inline-flex" }}><Icon name="shield" size={14} /></span>}
                        </strong>
                        {q.recommended ? <span className="badge badge-success">{q.status === "accepted" ? "Awarded" : "Best deal"}</span> : q.last_action === "walk_away" ? <span className="badge">Walked away</span> : q.status === "rejected" ? <span className="badge">Rejected</span> : null}
                      </div>
                      <div className="row" style={{ justifyContent: "space-between", marginTop: "0.45rem" }}>
                        <span className="price-move">
                          {drop > 0 && <span className="faint" style={{ textDecoration: "line-through", fontWeight: 500 }}>{money(q.initial_price_cents, q.currency)}</span>}
                          {money(q.price_cents, q.currency)}
                        </span>
                        <span className="small muted">{q.delivery_days != null ? `${q.delivery_days} days` : "—"} · v{q.version}</span>
                      </div>
                      {drop > 0 && <div className="small up" style={{ marginTop: "0.2rem" }}>−{money(drop, q.currency)} ({pct((drop / q.initial_price_cents) * 100)}) after {q.counters} counter{q.counters === 1 ? "" : "s"}</div>}
                      {s && (
                        <div style={{ marginTop: "0.5rem" }}>
                          <div className="row small faint" style={{ justifyContent: "space-between" }}><span>Deal score</span><span className="num">{Math.round(q.utility * 100)}</span></div>
                          <div className="bar" style={{ marginTop: "0.25rem" }}><span style={{ width: `${Math.max(2, q.utility * 100)}%`, background: q.recommended ? "var(--success)" : undefined }} /></div>
                        </div>
                      )}
                    </div>
                  );
                })}
            </div>
          )}
          {winner && s?.status === "completed" && (
            <p className="small muted" style={{ marginTop: "0.9rem" }}>Awarded to {winner.seller_name}. <Link href="/orders">See orders</Link></p>
          )}
        </section>
      </div>
    </>
  );
}

function RoundItem({ r, currency }: { r: Round; currency: string }) {
  const kind = r.action === "counter" ? "" : r.action === "accept_ready" ? " ok" : " bad";
  const icon = r.action === "counter" ? "arrowDown" : r.action === "accept_ready" ? "check" : "x";
  const title =
    r.action === "counter" ? `Countered ${r.seller_name}` : r.action === "accept_ready" ? `Acceptable offer from ${r.seller_name}` : `Walked away from ${r.seller_name}`;
  return (
    <li className="tl-item">
      <span className={`tl-dot${kind}`}><Icon name={icon} size={14} strokeWidth={2.2} /></span>
      <div className="tl-body">
        <div className="row wrap" style={{ justifyContent: "space-between", gap: "0.5rem" }}>
          <strong style={{ fontSize: "0.92rem" }}>{title}</strong>
          <span className="price-move small">
            {money(r.seller_price_cents, currency)}
            {r.offer_price_cents != null && (
              <>
                <Icon name="arrowRight" size={13} />
                <span style={{ color: "var(--primary)" }}>{money(r.offer_price_cents, currency)}</span>
              </>
            )}
          </span>
        </div>
        <div className="tl-meta">
          <span>Round {r.round}</span>·<span>quote v{r.responded_to_version}</span>·<span>score {Math.round(r.utility * 100)}</span>·
          <span>{new Date(r.created_at).toLocaleString()}</span>
        </div>
        <div className="small muted" style={{ marginTop: "0.3rem" }}>{r.rationale}</div>
        {r.message && <div className="tl-quote">“{r.message}”</div>}
      </div>
    </li>
  );
}

function MandateForm({ view, busy, onSubmit }: { view: NegotiationView; busy: boolean; onSubmit: (i: Parameters<typeof saveStrategy>[2]) => void }) {
  const s = view.strategy;
  const seed = s ?? view.suggested;
  const [target, setTarget] = useState(seed ? String(seed.target_price_cents / 100) : "");
  const [max, setMax] = useState(seed ? String(seed.max_price_cents / 100) : "");
  const [delivery, setDelivery] = useState(s?.max_delivery_days ? String(s.max_delivery_days) : "");
  const [weight, setWeight] = useState(s?.price_weight ?? 0.7);
  const [rounds, setRounds] = useState(s?.max_rounds ?? 4);
  const [tone, setTone] = useState<Tone>(s?.tone ?? "collaborative");
  const [auto, setAuto] = useState(s?.auto_accept ?? false);
  const [formErr, setFormErr] = useState<string | null>(null);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const t = dollarsToCents(target);
    const m = dollarsToCents(max);
    if (!t || !m) return setFormErr("Enter a target and walk-away price.");
    if (m < t) return setFormErr("Walk-away price must be at or above the target.");
    const d = delivery ? Number(delivery) : null;
    if (d !== null && (!Number.isInteger(d) || d <= 0)) return setFormErr("Delivery limit must be a whole number of days.");
    setFormErr(null);
    onSubmit({ target_price_cents: t, max_price_cents: m, max_delivery_days: d, price_weight: weight, max_rounds: rounds, tone, auto_accept: auto });
  };

  return (
    <form className="card" onSubmit={submit} style={{ marginBottom: "1rem" }}>
      <div className="section-head">
        <div>
          <h2 className="row" style={{ gap: "0.45rem" }}><Icon name="bot" /> Negotiation mandate</h2>
          <p className="small muted" style={{ margin: "0.2rem 0 0" }}>
            The agent counters each supplier between your target and walk-away price, using rival quotes as leverage.
            {view.suggested && !s && <> Suggested values start 12% under the best opening quote of {money(view.summary.current_best_cents, view.summary.currency)}.</>}
          </p>
        </div>
      </div>
      <div className="seg">
        <div className="field">
          <label htmlFor="m-target">Target price ({view.summary.currency || DEFAULT_CURRENCY})</label>
          <input id="m-target" className="input" inputMode="decimal" value={target} onChange={(e) => setTarget(e.target.value)} placeholder="e.g. 48,000" />
        </div>
        <div className="field">
          <label htmlFor="m-max">Walk-away price</label>
          <input id="m-max" className="input" inputMode="decimal" value={max} onChange={(e) => setMax(e.target.value)} placeholder="e.g. 54,000" />
        </div>
        <div className="field">
          <label htmlFor="m-del">Max delivery (days, optional)</label>
          <input id="m-del" className="input" inputMode="numeric" value={delivery} onChange={(e) => setDelivery(e.target.value)} placeholder="e.g. 14" />
        </div>
        <div className="field">
          <label htmlFor="m-rounds">Rounds per supplier: {rounds}</label>
          <input id="m-rounds" className="range" type="range" min={1} max={10} value={rounds} onChange={(e) => setRounds(Number(e.target.value))} />
        </div>
        <div className="field">
          <label htmlFor="m-weight">Priority: {Math.round(weight * 100)}% price / {100 - Math.round(weight * 100)}% delivery</label>
          <input id="m-weight" className="range" type="range" min={0} max={1} step={0.05} value={weight} onChange={(e) => setWeight(Number(e.target.value))} />
        </div>
        <div className="field">
          <label>Negotiation style</label>
          <div className="tabs" role="radiogroup" aria-label="Negotiation style">
            {(["collaborative", "firm"] as Tone[]).map((t) => (
              <button type="button" key={t} role="radio" aria-checked={tone === t} className={`tab${tone === t ? " on" : ""}`} onClick={() => setTone(t)} style={{ textTransform: "capitalize" }}>
                {t}
              </button>
            ))}
          </div>
          <span className="faint small">{tone === "firm" ? "Holds near target until late rounds." : "Concedes evenly each round."}</span>
        </div>
      </div>
      <label className="switch" style={{ marginTop: "1rem" }}>
        <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} />
        <span>
          <strong>Auto-accept the best deal</strong>
          <span className="muted small" style={{ display: "block" }}>Create the purchase order automatically when the agent closes within your walk-away price (Pro). Otherwise it waits for your approval.</span>
        </span>
      </label>
      {formErr && <p className="err" style={{ marginBottom: 0 }}>{formErr}</p>}
      <div className="row" style={{ marginTop: "1rem" }}>
        <button className="btn btn-primary" disabled={busy}>
          <Icon name="zap" size={16} /> {s ? "Update mandate" : "Start negotiating"}
        </button>
      </div>
    </form>
  );
}
