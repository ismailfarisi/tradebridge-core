"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { PageHeader } from "@/components/AppShell";
import { Icon } from "@/components/Icon";
import { useToken } from "@/lib/auth";
import {
  DEFAULT_CURRENCY,
  PRIORITIES,
  approveNegotiation,
  deadlineDays,
  declineNegotiation,
  describeLimits,
  dollarsToCents,
  explainPick,
  getNegotiation,
  money,
  parseLimits,
  pauseNegotiation,
  pct,
  quoteRange,
  resumeNegotiation,
  runNegotiation,
  saveStrategy,
  STATUS_LABEL,
  statusClass,
  suggestTarget,
  type NegotiationView,
  type Round,
  type Supplier,
  type Tone,
  when,
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
  const [chosenId, setChosenId] = useState<string | null>(null);
  const [confirmDecline, setConfirmDecline] = useState(false);

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

  const approve = async (quoteId?: string) => {
    if (!token) return;
    setBusy(true);
    setErr(null);
    try {
      const order = await approveNegotiation(token, id, quoteId);
      setOrderId(order.order_id);
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Approval failed");
    } finally {
      setBusy(false);
    }
  };

  const decline = () => {
    if (!token) return;
    act(() => declineNegotiation(token, id)).then(() => setConfirmDecline(false));
  };

  if (!view) return err ? <p className="err">{err}</p> : <p className="muted">Loading negotiation…</p>;

  const s = view.strategy;
  const cur = view.summary.currency || DEFAULT_CURRENCY;
  const winner = view.suppliers.find((x) => x.recommended);
  const closed = view.rfq.status === "closed";
  const showForm = !s || editing;
  const deciding = s?.status === "awaiting_approval" && !orderId;
  const done = s?.status === "completed" || !!orderId;
  const ranked = [...view.suppliers].sort((a, b) => (s ? b.utility - a.utility : a.price_cents - b.price_cents));
  const chosen = view.suppliers.find((x) => x.quote_id === (chosenId ?? winner?.quote_id));
  const overriding = !!chosen && !!winner && chosen.quote_id !== winner.quote_id;
  const rival = ranked.find((x) => x.quote_id !== winner?.quote_id && x.status === "submitted");
  const why =
    !s || !chosen || !winner
      ? ""
      : overriding
        ? explainPick(chosen, winner, s.price_weight)
        : rival
          ? explainPick(winner, rival, s.price_weight)
          : "It's the only supplier still in the running.";
  const awarded = view.suppliers.find((x) => x.status === "accepted");

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
        <div className="banner banner-ok" role="status">
          <Icon name="checkCircle" />
          <div className="grow"><strong>Deal approved.</strong> A purchase order was created.</div>
          <Link className="btn btn-sm" href={`/orders/${orderId}`}>View order</Link>
        </div>
      )}
      {s?.status === "completed" && !orderId && (
        <div className="banner banner-ok" role="status"><Icon name="checkCircle" /><div className="grow">{s.outcome_summary}</div></div>
      )}
      {s?.status === "cancelled" && (
        <div className="banner banner-info" role="status"><Icon name="x" /><div className="grow">{s.outcome_summary || "Negotiation ended without a deal."}</div></div>
      )}

      <section className={`card ledger-card${deciding ? " is-deciding" : ""}`} aria-labelledby="ledger-title">
        <div className="section-head">
          <div>
            <h2 id="ledger-title">{deciding ? "Ready for your approval" : "Suppliers"}</h2>
            {deciding && winner && (
              <p className="small muted" style={{ margin: "0.2rem 0 0" }}>
                The agent recommends {winner.seller_name}. Choose another supplier if you prefer.
              </p>
            )}
          </div>
        </div>
        {ranked.length === 0 ? (
          <p className="muted small">Waiting for supplier quotes.</p>
        ) : (
          <div className="ledger" role={deciding ? "radiogroup" : undefined} aria-label={deciding ? "Supplier to award" : undefined}>
            <div className="ledger-row ledger-head" aria-hidden="true">
              <span />
              <span>Supplier</span>
              <span className="num">Price</span>
              <span>Delivery</span>
              <span className="num">Saved</span>
              <span>Status</span>
            </div>
            {ranked.map((q, i) => (
              <LedgerRow
                key={q.quote_id}
                q={q}
                rank={i + 1}
                selectable={deciding && q.status === "submitted"}
                selected={deciding && chosen?.quote_id === q.quote_id}
                onSelect={() => setChosenId(q.quote_id)}
              />
            ))}
          </div>
        )}
        {deciding && chosen && (
          <div className="decision">
            <p className="decision-why">
              <strong>{overriding ? "You're overriding the agent. " : "Why this deal: "}</strong>
              {why}
            </p>
            <div className="row wrap" style={{ gap: "0.6rem" }}>
              <button className="btn btn-success" disabled={busy} onClick={() => approve(overriding ? chosen.quote_id : undefined)}>
                <Icon name="check" size={16} />
                <span>
                  {overriding ? "Award" : "Approve"} {chosen.seller_name} · <span className="num">{money(chosen.price_cents, chosen.currency)}</span>
                </span>
              </button>
              {confirmDecline ? (
                <span className="row wrap" style={{ gap: "0.5rem" }}>
                  <span className="small">No order will be placed. Quotes stay open.</span>
                  <button className="btn btn-danger btn-sm" disabled={busy} onClick={decline}>Decline deal</button>
                  <button className="btn btn-ghost btn-sm" onClick={() => setConfirmDecline(false)}>Keep</button>
                </span>
              ) : (
                <button className="btn btn-ghost" disabled={busy} onClick={() => setConfirmDecline(true)}>Decline</button>
              )}
            </div>
            <p className="small faint" style={{ margin: 0 }}>Approving creates a purchase order with the chosen supplier.</p>
          </div>
        )}
        {awarded && !deciding && (
          <p className="small muted" style={{ marginTop: "0.9rem" }}>Awarded to {awarded.seller_name}. <Link href="/orders">See orders</Link></p>
        )}
      </section>

      {s && (
        <div className="kpis">
          <div className="kpi">
            <div className="kpi-label"><Icon name="file" size={16} /> Best opening quote</div>
            <div className="kpi-value">{money(view.summary.initial_best_cents || null, cur)}</div>
          </div>
          <div className="kpi">
            <div className="kpi-label"><Icon name="target" size={16} /> Your target</div>
            <div className="kpi-value">{s ? money(s.target_price_cents, cur) : "—"}</div>
            {s && <div className="kpi-sub">Walk away above {money(s.max_price_cents, cur)}</div>}
          </div>
          <div className="kpi">
            <div className="kpi-label"><Icon name="handshake" size={16} /> {done ? "Agreed price" : "Best current offer"}</div>
            <div className="kpi-value">{money(view.summary.current_best_cents || null, cur)}</div>
          </div>
          <div className="kpi kpi-accent">
            <div className="kpi-label"><Icon name="trending" size={16} /> {done ? "Saved" : "Saved so far"}</div>
            <div className="kpi-value">{money(view.summary.savings_cents, cur)}</div>
            <div className="kpi-sub">{pct(view.summary.savings_pct)} vs opening</div>
          </div>
        </div>
      )}

      {showForm && token && (
        <MandateForm
          view={view}
          busy={busy}
          onSubmit={(input) => act(() => saveStrategy(token, id, input))}
        />
      )}

      {(s || view.rounds.length > 0) && (
        <section className="card">
          <div className="section-head">
            <h2>Agent moves by supplier</h2>
            {live && <span className="faint small">Updates every {LIVE_POLL_MS / 1000}s</span>}
          </div>
          {view.rounds.length === 0 ? (
            <div className="empty">
              <Icon name="bot" size={28} />
              <p style={{ margin: 0 }}>{s ? "The agent will respond as soon as suppliers quote." : "Set a mandate and the agent will counter every supplier."}</p>
            </div>
          ) : (
            <div className="log-groups">
              {groupRounds(view.rounds, ranked).map((g) => <LogGroup key={g.quoteId} name={g.name} rounds={g.rounds} currency={cur} />)}
            </div>
          )}
        </section>
      )}
    </>
  );
}

// groupRounds buckets rounds per supplier quote, newest move first, ordered
// like the supplier table.
function groupRounds(rounds: Round[], ranked: Supplier[]) {
  const order = new Map(ranked.map((q, i) => [q.quote_id, i]));
  const groups = new Map<string, { quoteId: string; name: string; rounds: Round[] }>();
  for (const r of rounds) {
    const g = groups.get(r.quote_id) ?? { quoteId: r.quote_id, name: r.seller_name, rounds: [] };
    g.rounds.push(r);
    groups.set(r.quote_id, g);
  }
  return [...groups.values()]
    .map((g) => ({ ...g, rounds: [...g.rounds].sort((a, b) => b.created_at.localeCompare(a.created_at) || b.round - a.round) }))
    .sort((a, b) => (order.get(a.quoteId) ?? 99) - (order.get(b.quoteId) ?? 99));
}

function LogGroup({ name, rounds, currency }: { name: string; rounds: Round[]; currency: string }) {
  const [open, setOpen] = useState(false);
  const shown = open ? rounds : rounds.slice(0, 1);
  return (
    <div className="log-group">
      <div className="log-group-head">
        <strong>{name}</strong>
        <span className="faint small">{rounds.length} agent move{rounds.length === 1 ? "" : "s"}</span>
      </div>
      <ol className="timeline">
        {shown.map((r) => <RoundItem key={r.id} r={r} currency={currency} />)}
      </ol>
      {rounds.length > 1 && (
        <button type="button" className="btn btn-ghost btn-sm" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
          {open ? "Show latest move only" : `Show all ${rounds.length} moves`}
        </button>
      )}
    </div>
  );
}

function supplierBadge(q: Supplier) {
  if (q.status === "accepted") return <span className="badge badge-success">Awarded</span>;
  if (q.status === "rejected") return <span className="badge">Rejected</span>;
  if (q.status === "withdrawn") return <span className="badge">Withdrawn</span>;
  if (q.recommended) return <span className="badge badge-success">Agent&apos;s pick</span>;
  if (q.last_action === "walk_away") return <span className="badge badge-warning">Above walk-away</span>;
  return <span className="badge">In talks</span>;
}

function LedgerRow({ q, rank, selectable, selected, onSelect }: { q: Supplier; rank: number; selectable: boolean; selected: boolean; onSelect: () => void }) {
  const drop = q.initial_price_cents - q.price_cents;
  const className = `ledger-row${q.recommended || q.status === "accepted" ? " is-pick" : ""}${selected ? " is-selected" : ""}`;
  const cells = (
    <>
      <span className="ledger-rank">
        {selectable ? (
          <input type="radio" name="award" checked={selected} onChange={onSelect} aria-label={`Award ${q.seller_name}`} />
        ) : (
          <span className="faint num">{rank}</span>
        )}
      </span>
      <span className="ledger-supplier">
        <strong>{q.seller_name}</strong>
        {q.verification_status === "verified" && (
          <span title="Verified supplier" aria-label="Verified supplier" className="ledger-verified"><Icon name="shield" size={14} /></span>
        )}
      </span>
      <span className="ledger-price num">
        {drop > 0 && <s className="faint">{money(q.initial_price_cents, q.currency)}</s>}
        <strong>{money(q.price_cents, q.currency)}</strong>
      </span>
      <span className="ledger-delivery small">{q.delivery_days != null ? `${q.delivery_days} days` : "—"}</span>
      <span className="ledger-saved small num">
        {drop > 0 ? <span className="up">−{money(drop, q.currency)} ({pct((drop / q.initial_price_cents) * 100)})</span> : <span className="faint">—</span>}
        <span className="faint">{q.counters} counter{q.counters === 1 ? "" : "s"}</span>
      </span>
      <span className="ledger-status">{supplierBadge(q)}</span>
    </>
  );
  return selectable ? <label className={className}>{cells}</label> : <div className={className}>{cells}</div>;
}

function RoundItem({ r, currency }: { r: Round; currency: string }) {
  const kind = r.action === "counter" ? "" : r.action === "accept_ready" ? " ok" : " bad";
  const icon = r.action === "counter" ? "arrowDown" : r.action === "accept_ready" ? "check" : "x";
  const title =
    r.action === "counter" ? "Countered" : r.action === "accept_ready" ? "Within your limits" : "Walked away";
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
          <span>Round {r.round}</span>·<time dateTime={r.created_at}>{when(r.created_at)}</time>
        </div>
        <div className="small muted" style={{ marginTop: "0.3rem" }}>{r.rationale}</div>
        {r.message && <div className="tl-quote">“{r.message}”</div>}
      </div>
    </li>
  );
}

function MandateForm({ view, busy, onSubmit }: { view: NegotiationView; busy: boolean; onSubmit: (i: Parameters<typeof saveStrategy>[2]) => void }) {
  const s = view.strategy;
  const cur = view.summary.currency || DEFAULT_CURRENCY;
  const range = quoteRange(view.suppliers);
  const best = view.summary.current_best_cents || range?.minCents || 0;
  const seed = s ?? view.suggested;
  const fromRfq = s ? undefined : deadlineDays(view.rfq.deadline ?? "");
  const [text, setText] = useState("");
  const [max, setMax] = useState(seed ? String(seed.max_price_cents / 100) : "");
  const [delivery, setDelivery] = useState(s?.max_delivery_days ? String(s.max_delivery_days) : fromRfq ? String(fromRfq) : "");
  const [weight, setWeight] = useState(s?.price_weight ?? 0.7);
  const [targetText, setTargetText] = useState(s ? String(s.target_price_cents / 100) : "");
  const [rounds, setRounds] = useState(s?.max_rounds ?? 4);
  const [tone, setTone] = useState<Tone>(s?.tone ?? "collaborative");
  const [auto, setAuto] = useState(s?.auto_accept ?? false);
  const [formErr, setFormErr] = useState<string | null>(null);

  const maxCents = dollarsToCents(max);
  const cap = maxCents && best ? Math.min(maxCents, best) : maxCents || best;
  const autoTarget = cap ? suggestTarget(cap) : null;
  const targetCents = targetText.trim() ? dollarsToCents(targetText) : autoTarget;
  const days = delivery.trim() ? Number(delivery) : null;
  const understood = text.trim() ? parseLimits(text) : null;
  const priority = PRIORITIES.find((p) => p.weight === weight);
  const belowAll = !!maxCents && !!range && maxCents < range.minCents;

  const describe = (v: string) => {
    setText(v);
    const l = parseLimits(v);
    if (l.maxCents) setMax(String(l.maxCents / 100));
    if (l.days) setDelivery(String(l.days));
    if (l.weight != null) setWeight(l.weight);
  };

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!maxCents) return setFormErr("Enter the most you'll pay.");
    if (!targetCents) return setFormErr("Enter an opening target, or leave it blank to use the suggestion.");
    if (maxCents < targetCents) return setFormErr("The opening target must be at or below the most you'll pay.");
    if (days !== null && (!Number.isInteger(days) || days <= 0)) return setFormErr("Delivery limit must be a whole number of days.");
    setFormErr(null);
    onSubmit({ target_price_cents: targetCents, max_price_cents: maxCents, max_delivery_days: days, price_weight: weight, max_rounds: rounds, tone, auto_accept: auto });
  };

  const plan =
    maxCents && targetCents
      ? `The agent opens near ${money(targetCents, cur)} and won't agree to more than ${money(maxCents, cur)}${days ? `, with delivery within ${days} days` : ""}. ${
          auto ? "If it closes a deal, the order is placed automatically." : "You approve the final deal before any order is placed."
        }`
      : "Enter the most you'll pay to see the agent's plan.";

  return (
    <form className="card mandate" onSubmit={submit} style={{ marginBottom: "1rem" }}>
      <div className="section-head">
        <div>
          <h2 className="row" style={{ gap: "0.45rem" }}><Icon name="bot" /> {s ? "Agent's limits" : "Set the agent's limits"}</h2>
          <p className="small muted" style={{ margin: "0.2rem 0 0" }}>The agent haggles with each supplier inside these limits and never goes past them.</p>
        </div>
      </div>
      {range && (
        <p className="mandate-market small">
          {range.count} quote{range.count === 1 ? "" : "s"} so far:{" "}
          <span className="num">
            {money(range.minCents, cur)}
            {range.maxCents !== range.minCents && <> – {money(range.maxCents, cur)}</>}
          </span>
          {range.minDays != null && <>, delivery in {range.minDays === range.maxDays ? range.minDays : `${range.minDays}–${range.maxDays}`} days</>}.
        </p>
      )}
      <div className="field">
        <label htmlFor="m-text">Describe your limits <span className="faint">(optional)</span></label>
        <textarea id="m-text" className="input" rows={2} value={text} onChange={(e) => describe(e.target.value)} placeholder="e.g. Up to AED 50k, need it within 2 weeks, price matters most" />
        {understood && (
          <span className="field-hint" aria-live="polite">
            {describeLimits(understood, cur) || "No amount, timeframe or priority found. Fill in the fields below."}
          </span>
        )}
      </div>
      <div className="seg">
        <div className="field">
          <label htmlFor="m-max">Most you&apos;ll pay ({cur})</label>
          <input id="m-max" className="input" inputMode="decimal" value={max} onChange={(e) => setMax(e.target.value)} placeholder="e.g. 50,000" />
          {belowAll && <span className="field-hint warn-text">Below every quote so far, so the agent may walk away from all suppliers.</span>}
        </div>
        <div className="field">
          <label htmlFor="m-del">Need it within (days)</label>
          <input id="m-del" className="input" inputMode="numeric" value={delivery} onChange={(e) => setDelivery(e.target.value)} placeholder="Any time" />
          {fromRfq && delivery === String(fromRfq) && view.rfq.deadline && <span className="field-hint">From your request: &ldquo;{view.rfq.deadline}&rdquo;</span>}
        </div>
      </div>
      <div className="field">
        <label id="m-prio">What matters more?</label>
        <div className="tabs" role="radiogroup" aria-labelledby="m-prio">
          {PRIORITIES.map((p) => (
            <button type="button" key={p.key} role="radio" aria-checked={priority?.key === p.key} className={`tab${priority?.key === p.key ? " on" : ""}`} onClick={() => setWeight(p.weight)}>
              {p.label}
            </button>
          ))}
        </div>
        {!priority && <span className="field-hint">Custom: {Math.round(weight * 100)}% price, {100 - Math.round(weight * 100)}% delivery</span>}
      </div>
      <p className="mandate-plan" aria-live="polite">{plan}</p>
      <details className="mandate-more" open={!!s}>
        <summary>Fine-tune</summary>
        <div className="seg">
          <div className="field">
            <label htmlFor="m-target">Opening target ({cur})</label>
            <input id="m-target" className="input" inputMode="decimal" value={targetText} onChange={(e) => setTargetText(e.target.value)} placeholder={autoTarget ? `Suggested: ${(autoTarget / 100).toLocaleString("en-US")}` : "e.g. 44,000"} />
            <span className="field-hint">Where the agent starts. Leave blank to start about 12% under the best quote.</span>
          </div>
          <div className="field">
            <label htmlFor="m-rounds">Counters per supplier: {rounds}</label>
            <input id="m-rounds" className="range" type="range" min={1} max={10} value={rounds} onChange={(e) => setRounds(Number(e.target.value))} />
            <span className="field-hint">After this many, the agent takes the supplier&apos;s best offer or walks away.</span>
          </div>
          <div className="field">
            <label htmlFor="m-weight">Price vs delivery: {Math.round(weight * 100)}% / {100 - Math.round(weight * 100)}%</label>
            <input id="m-weight" className="range" type="range" min={0} max={1} step={0.05} value={weight} onChange={(e) => setWeight(Number(e.target.value))} />
            <span className="field-hint">How the agent ranks a cheaper offer against a faster one.</span>
          </div>
          <div className="field">
            <label id="m-tone">Negotiation style</label>
            <div className="tabs" role="radiogroup" aria-labelledby="m-tone">
              {(["collaborative", "firm"] as Tone[]).map((t) => (
                <button type="button" key={t} role="radio" aria-checked={tone === t} className={`tab${tone === t ? " on" : ""}`} onClick={() => setTone(t)} style={{ textTransform: "capitalize" }}>
                  {t}
                </button>
              ))}
            </div>
            <span className="field-hint">{tone === "firm" ? "Stays near your target until the last counters." : "Moves a little each counter."}</span>
          </div>
        </div>
        <label className="switch" style={{ marginTop: "0.5rem" }}>
          <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} />
          <span>
            <strong>Place the order automatically</strong>
            <span className="muted small" style={{ display: "block" }}>When the agent closes a deal within your limits (Pro). Otherwise it waits for your approval.</span>
          </span>
        </label>
      </details>
      {formErr && <p className="err" style={{ marginBottom: 0 }}>{formErr}</p>}
      <div className="row" style={{ marginTop: "1rem" }}>
        <button className="btn btn-primary" disabled={busy}>
          <Icon name="zap" size={16} /> {s ? "Update limits" : "Start negotiating"}
        </button>
      </div>
    </form>
  );
}
