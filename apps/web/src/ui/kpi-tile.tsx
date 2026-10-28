import type { ReactNode } from 'react';
import { cn } from './cn';

/** The fill of a bar: run is the flow, ok and amber are the good and warning ends, rework is the bad one. */
export type KpiTone = 'run' | 'ok' | 'rework' | 'amber';

const fillOf: Record<KpiTone, string> = {
  run: 'bg-run',
  ok: 'bg-ok',
  rework: 'bg-rework',
  amber: 'bg-amber',
};

/** A change against the last period. `good` only picks the colour: the arrow and the sign carry the direction. */
export interface KpiDelta {
  value: number;
  unit?: string;
  good: boolean;
}

/** The signed change: "▲ +2 pts", "▼ −0.6". Zero has no arrow. */
export function Delta({ value, unit, good }: KpiDelta) {
  const suffix = unit ? ` ${unit}` : '';
  if (value === 0) return <span className="text-muted">0{suffix}</span>;
  const up = value > 0;
  const sign = up ? '+' : '−';
  return (
    <span className={cn('font-semibold', good ? 'text-ok-ink' : 'text-rework-ink')}>
      <span aria-hidden="true">{up ? '▲ ' : '▼ '}</span>
      {`${sign}${Math.abs(value)}${suffix}`}
    </span>
  );
}

/** A bar against a target: a `meter` named by `label`. */
export interface KpiBar {
  value: number;
  /** The end of the track: the target, or the figure that stands for full. */
  max: number;
  tone: KpiTone;
  /** The meter's accessible name: "76% of the target of 10 a day". */
  label: string;
}

/** A trend: the points run left to right, and `label` says what the line shows. */
export interface KpiSparkline {
  points: readonly number[];
  label: string;
}

/**
 * A flow figure on its own card: the label, the number with its unit, a bar against the target or
 * a sparkline, then the target on the left and the signed delta (or a note) on the right.
 */
export function KpiTile({
  label,
  value,
  unit,
  bar,
  sparkline,
  target,
  delta,
  note,
  className,
}: {
  label: ReactNode;
  value: ReactNode;
  unit?: string | undefined;
  bar?: KpiBar | undefined;
  sparkline?: KpiSparkline | undefined;
  /** The left of the meta line: "Target 10". */
  target?: ReactNode;
  delta?: KpiDelta | undefined;
  /** The right of the meta line when there is no delta. */
  note?: ReactNode;
  className?: string | undefined;
}) {
  return (
    <div
      className={cn('flex min-w-0 flex-col gap-2 rounded-card border border-line bg-card px-4 pt-4 pb-3', className)}
    >
      <span className="text-secondary text-ink-soft">{label}</span>
      <p className="m-0 flex items-baseline text-kpi font-numeral tracking-numeral tabular-nums">
        {value}
        {unit ? <span className="ml-1 text-body font-medium text-ink-soft">{unit}</span> : null}
      </p>
      {bar ? <KpiMeter bar={bar} /> : null}
      {sparkline ? <Sparkline {...sparkline} /> : null}
      <div className="flex items-center justify-between gap-3 text-meta text-muted tabular-nums">
        <span className="min-w-0">{target}</span>
        {delta ? <Delta {...delta} /> : note ? <span className="min-w-0 text-right">{note}</span> : null}
      </div>
    </div>
  );
}

function KpiMeter({ bar }: { bar: KpiBar }) {
  const pct = bar.max > 0 ? Math.min(100, Math.max(0, (bar.value / bar.max) * 100)) : 0;
  return (
    // A styled track, so the ARIA meter is set by hand as target-bar.tsx does (a native meter takes no fill styling).
    <div
      {...{
        role: 'meter',
        'aria-label': bar.label,
        'aria-valuemin': 0,
        'aria-valuemax': bar.max,
        'aria-valuenow': bar.value,
      }}
      className="h-1.5 overflow-hidden rounded-full bg-hover"
    >
      <div className={cn('h-full rounded-full', fillOf[bar.tone])} style={{ width: `${pct}%` }} />
    </div>
  );
}

/** A trend line over its points, a `img` named by `label`. The table's cells size it with `className`. */
export function Sparkline({ points, label, className }: KpiSparkline & { className?: string }) {
  const min = Math.min(...points);
  const span = Math.max(...points) - min || 1;
  const step = points.length > 1 ? 100 / (points.length - 1) : 0;
  // Two units of room above and below the line, so the stroke is not clipped at the edges.
  const line = points
    .map((p, i) => `${(i * step).toFixed(2)},${(2 + (1 - (p - min) / span) * 28).toFixed(2)}`)
    .join(' ');
  return (
    <svg
      viewBox="0 0 100 32"
      preserveAspectRatio="none"
      role="img"
      aria-label={label}
      className={cn('block h-8 w-full text-run', className)}
    >
      <polyline
        points={line}
        fill="none"
        stroke="currentColor"
        strokeWidth={2}
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}
