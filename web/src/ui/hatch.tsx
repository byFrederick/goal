import { useId } from 'react';
import { cn } from './cn';

const palettes = {
  /** Actual: teal on pale teal. */
  run: { stripe: 'var(--color-run)', ground: 'var(--color-run-hatch)' },
  /** Delay, loss, rework: rust. */
  rework: { stripe: 'var(--color-rework)', ground: 'var(--color-rework-hatch)' },
  /** Waiting on a human: violet. */
  you: { stripe: 'var(--color-you)', ground: 'var(--color-you-line)' },
  /** Target or neutral time: gray. */
  target: { stripe: 'var(--color-target)', ground: 'var(--color-line)' },
} as const;

export type HatchTone = keyof typeof palettes;

/**
 * Fills its box with the design system's 135° hatch (MASTER.md data marks), drawn as an SVG
 * pattern so no gradient is involved. Decorative: the caller labels the value in text.
 */
export function Hatch({ tone = 'run', className }: { tone?: HatchTone; className?: string }) {
  const id = useId();
  const { stripe, ground } = palettes[tone];
  return (
    <svg aria-hidden="true" className={cn('block size-full', className)} preserveAspectRatio="none">
      <defs>
        <pattern id={id} width="5" height="5" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
          <rect width="5" height="5" fill={ground} />
          <rect width="2" height="5" fill={stripe} />
        </pattern>
      </defs>
      <rect width="100%" height="100%" fill={`url(#${CSS.escape(id)})`} />
    </svg>
  );
}

/**
 * A horizontal meter: a hatched fill to `value` of `max`, with an optional target tick. Without
 * `label` it is decorative (the caller states the value in text); with one it is a `meter` named
 * by it ("$18.40 of $60 weekly budget, 31%").
 */
export function Meter({
  value,
  max,
  target,
  tone = 'run',
  label,
  className,
}: {
  value: number;
  max: number;
  target?: number;
  tone?: HatchTone;
  label?: string;
  className?: string;
}) {
  const pct = max > 0 ? Math.min(100, Math.max(0, (value / max) * 100)) : 0;
  const meter = label
    ? {
        role: 'meter',
        'aria-label': label,
        'aria-valuemin': 0,
        'aria-valuemax': max,
        'aria-valuenow': Math.min(Math.max(0, value), max),
      }
    : { 'aria-hidden': true };
  return (
    <div {...meter} className={cn('relative h-2.5 rounded-[3px] bg-surface', className)}>
      {pct > 0 ? (
        <div className="h-full overflow-hidden rounded-[3px]" style={{ width: `${pct}%` }}>
          <Hatch tone={tone} />
        </div>
      ) : null}
      {target !== undefined && max > 0 ? (
        <span
          className="absolute -top-1.5 h-[calc(100%+12px)] w-0.5 rounded-[1px] bg-ink"
          style={{ left: `${Math.min(100, (target / max) * 100)}%` }}
        />
      ) : null}
    </div>
  );
}

/**
 * A bar sparkline: one thin hatched teal bar per value on a shared baseline, the last one (the
 * latest) solid; a zero is a flat stub, never a bar. `label` gives screen readers the values.
 */
export function HatchBars({
  bars,
  label,
  height = 28,
  className,
}: {
  bars: readonly { key: string; value: number }[];
  label: string;
  /** The tallest bar, in px. */
  height?: number;
  className?: string;
}) {
  const max = Math.max(0, ...bars.map((b) => b.value));
  return (
    <span role="img" aria-label={label} className={cn('flex shrink-0 items-end gap-0.5', className)} style={{ height }}>
      {bars.map((b, i) => {
        if (b.value <= 0 || max === 0) return <span key={b.key} className="h-0.5 w-[5px] rounded-[1px] bg-line" />;
        const latest = i === bars.length - 1;
        return (
          <span
            key={b.key}
            className={cn('w-[5px] overflow-hidden rounded-[1px]', latest && 'bg-run')}
            style={{ height: Math.max(3, Math.round((b.value / max) * height)) }}
          >
            {latest ? null : <Hatch tone="run" />}
          </span>
        );
      })}
    </span>
  );
}
