import { cn } from './cn';

export type MeterTone = 'ink' | 'rework';

const fillOf: Record<MeterTone, string> = { ink: 'bg-ink', rework: 'bg-rework' };

/**
 * A horizontal meter: a solid fill to `value` of `max` on a `hover` track, with an optional ink
 * target tick. The fill grows from the start with a scale transform. Without `label` it is
 * decorative (the caller states the value in text); with one it is a `meter` named by it
 * ("$18.40 of $60 weekly budget, 31%").
 */
export function Meter({
  value,
  max,
  target,
  tone = 'ink',
  label,
  className,
}: {
  value: number;
  max: number;
  target?: number;
  tone?: MeterTone;
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
    <div {...meter} className={cn('relative h-1.5 rounded-chip bg-hover', className)}>
      <div className="h-full overflow-hidden rounded-chip">
        <div
          className={cn(
            'h-full origin-left rounded-chip transition-transform duration-(--duration-fast) ease-out motion-reduce:transition-none',
            fillOf[tone],
          )}
          style={{ transform: `scaleX(${pct / 100})` }}
        />
      </div>
      {target !== undefined && max > 0 ? (
        <span
          aria-hidden="true"
          className="absolute -top-1 h-[calc(100%+8px)] w-0.5 bg-ink"
          style={{ left: `${Math.min(100, (target / max) * 100)}%` }}
        />
      ) : null}
    </div>
  );
}
