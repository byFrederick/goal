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

/** A horizontal meter: a hatched fill to `value` of `max`, with an optional target tick. */
export function Meter({
  value,
  max,
  target,
  tone = 'run',
  className,
}: {
  value: number;
  max: number;
  target?: number;
  tone?: HatchTone;
  className?: string;
}) {
  const pct = max > 0 ? Math.min(100, Math.max(0, (value / max) * 100)) : 0;
  return (
    <div aria-hidden="true" className={cn('relative h-2.5 rounded-[3px] bg-surface', className)}>
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
