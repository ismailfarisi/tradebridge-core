import { DEFAULT_CURRENCY, money, monthLabel, type Savings } from "@/lib/negotiation";

// SpendChart is a dependency-free SVG stacked bar chart: negotiated spend with
// the savings captured on top, one bar per month.
export function SpendChart({ data, currency = DEFAULT_CURRENCY, height = 220 }: { data: Savings["monthly"]; currency?: string; height?: number }) {
  const W = 720;
  const padL = 56;
  const padB = 26;
  const padT = 10;
  const max = Math.max(1, ...data.map((d) => d.spend_cents + d.savings_cents));
  const step = niceStep(max / 4);
  const top = Math.ceil(max / step) * step;
  const plotH = height - padB - padT;
  const slot = (W - padL) / Math.max(1, data.length);
  const bw = Math.min(34, slot * 0.56);
  const y = (v: number) => padT + plotH - (v / top) * plotH;
  const ticks = Array.from({ length: Math.round(top / step) + 1 }, (_, i) => i * step);

  return (
    <figure style={{ margin: 0 }}>
      <svg viewBox={`0 0 ${W} ${height}`} width="100%" role="img" aria-label="Monthly negotiated spend and savings">
        {ticks.map((t) => (
          <g key={t}>
            <line x1={padL} x2={W} y1={y(t)} y2={y(t)} stroke="var(--border)" strokeDasharray={t ? "3 4" : undefined} />
            <text x={padL - 8} y={y(t) + 4} textAnchor="end" fontSize="11" fill="var(--faint)">
              {money(t, currency, { compact: true })}
            </text>
          </g>
        ))}
        {data.map((d, i) => {
          const x = padL + slot * i + (slot - bw) / 2;
          const spendTop = y(d.spend_cents);
          const saveTop = y(d.spend_cents + d.savings_cents);
          return (
            <g key={d.month}>
              <title>{`${monthLabel(d.month)}: ${money(d.spend_cents, currency)} spend · ${money(d.savings_cents, currency)} saved`}</title>
              {d.spend_cents > 0 && <rect x={x} y={spendTop} width={bw} height={y(0) - spendTop} rx={4} fill="var(--primary)" />}
              {d.savings_cents > 0 && <rect x={x} y={saveTop} width={bw} height={Math.max(2, spendTop - saveTop)} rx={4} fill="#22c55e" />}
              <text x={x + bw / 2} y={height - 8} textAnchor="middle" fontSize="11" fill="var(--faint)">
                {monthLabel(d.month)}
              </text>
            </g>
          );
        })}
      </svg>
      <figcaption className="row small muted" style={{ gap: "1rem", marginTop: "0.4rem" }}>
        <Legend color="var(--primary)" label="Negotiated spend" />
        <Legend color="#22c55e" label="Savings vs opening quotes" />
      </figcaption>
    </figure>
  );
}

function Legend({ color, label }: { color: string; label: string }) {
  return (
    <span className="row" style={{ gap: "0.4rem" }}>
      <span style={{ width: 10, height: 10, borderRadius: 3, background: color, display: "inline-block" }} />
      {label}
    </span>
  );
}

function niceStep(raw: number): number {
  const mag = Math.pow(10, Math.floor(Math.log10(Math.max(raw, 1))));
  const n = raw / mag;
  return (n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10) * mag;
}
