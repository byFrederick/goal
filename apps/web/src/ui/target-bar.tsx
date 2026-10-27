import { cn } from './cn';
import { Hatch } from './hatch';

/** The ARIA meter of a styled track (a native meter cannot carry the hatch and the tick). */
const meterOf = (label: string, value: number, max: number) => ({
  role: 'meter',
  'aria-label': label,
  'aria-valuemin': 0,
  'aria-valuemax': max,
  'aria-valuenow': value,
});

/**
 * Actual against a target on one track: hatched teal to `value` and an ink tick at `target`;
 * from 768 px, "0" under the start and the target's label under the tick (phones say the target
 * in the text beside it). The track runs to a quarter past the target, or to the value when it is
 * beyond that. A `meter` named by `label`.
 */
export function TargetBar({
  value,
  target,
  targetLabel,
  label,
  className,
}: {
  value: number;
  target: number;
  /** Under the tick: "Target 8". */
  targetLabel: string;
  /** The meter's accessible name: "6.4 a day against a target of 8". */
  label: string;
  className?: string;
}) {
  const max = Math.max(target * 1.25, value);
  const pct = (n: number) => (max > 0 ? Math.min(100, Math.max(0, (n / max) * 100)) : 0);
  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <div {...meterOf(label, value, max)} className="relative h-2.5 rounded-[4px] bg-card md:h-3">
        {value > 0 ? (
          <div className="h-full overflow-hidden rounded-[4px]" style={{ width: `${pct(value)}%` }}>
            <Hatch tone="run" />
          </div>
        ) : null}
        <span
          aria-hidden="true"
          className="absolute -top-1 h-[calc(100%+8px)] w-0.5 -translate-x-1/2 rounded-[1px] bg-ink md:-top-1.5 md:h-[calc(100%+12px)]"
          style={{ left: `${pct(target)}%` }}
        />
      </div>
      <div aria-hidden="true" className="relative h-[18px] text-[13px] text-muted tabular-nums max-md:hidden">
        <span>0</span>
        <span className="absolute -translate-x-1/2 whitespace-nowrap" style={{ left: `${pct(target)}%` }}>
          {targetLabel}
        </span>
      </div>
    </div>
  );
}
