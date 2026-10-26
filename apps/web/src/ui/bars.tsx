import { cn } from './cn';
import { Hatch } from './hatch';

export interface Bar {
  key: string;
  value: number;
  /** What the bar is: "Oct 10 to 16". */
  label: string;
  /** Its value in words: "1.4 a day, 10 finished". */
  detail: string;
}

/**
 * Vertical bars on one baseline, oldest first: hatched teal, the latest solid; a zero is a flat
 * stub. A dashed ink line marks the target, labelled at its right end. The scale tops out at the
 * target or the tallest bar. Hovering a bar shows its label and detail; the chart is an image
 * named by `summary`, and a visually hidden table (`caption`) carries every value for screen
 * readers. Bars grow without motion under prefers-reduced-motion.
 */
export function Bars({
  bars,
  target,
  targetLabel,
  summary,
  caption,
  columns,
  className,
}: {
  bars: readonly Bar[];
  target?: number | null;
  targetLabel?: string;
  summary: string;
  caption: string;
  /** The hidden table's column headers. */
  columns: readonly [string, string];
  className?: string;
}) {
  const max = Math.max(target ?? 0, ...bars.map((b) => b.value));
  const pct = (n: number) => (max > 0 ? (n / max) * 100 : 0);
  return (
    <figure className={cn('m-0 flex flex-col', className)}>
      <div role="img" aria-label={summary} className="relative flex h-full min-h-[140px] items-end gap-1.5 md:gap-2">
        {target ? (
          <>
            <span
              aria-hidden="true"
              className="pointer-events-none absolute inset-x-0 border-t-[1.5px] border-dashed border-ink"
              style={{ bottom: `${pct(target)}%` }}
            />
            <span
              aria-hidden="true"
              className="pointer-events-none absolute right-0 mb-1 text-[13px] text-ink"
              style={{ bottom: `${pct(target)}%` }}
            >
              {targetLabel}
            </span>
          </>
        ) : null}
        {bars.map((b, i) => {
          const latest = i === bars.length - 1;
          return (
            <div key={b.key} className="group relative flex h-full min-w-0 flex-1 items-end">
              {b.value > 0 ? (
                <div
                  className={cn(
                    'w-full overflow-hidden rounded-t-[3px] rounded-b-[1px] transition-[height] duration-300 motion-reduce:transition-none',
                    latest && 'bg-run',
                  )}
                  style={{ height: `${pct(b.value)}%` }}
                >
                  {latest ? null : <Hatch tone="run" />}
                </div>
              ) : (
                <div className="h-0.5 w-full rounded-[1px] bg-line" />
              )}
              <span
                aria-hidden="true"
                className={cn(
                  'pointer-events-none absolute bottom-full z-10 mb-1.5 hidden rounded-chip border border-rule bg-surface px-2.5 py-1.5 text-[13px] whitespace-nowrap text-ink shadow-tip group-hover:block',
                  // The tip stays inside the chart at both ends.
                  i < 2 ? 'left-0' : i >= bars.length - 2 ? 'right-0' : 'left-1/2 -translate-x-1/2',
                )}
              >
                <span className="block font-medium">{b.label}</span>
                <span className="block text-muted tabular-nums">{b.detail}</span>
              </span>
            </div>
          );
        })}
      </div>
      <table className="sr-only">
        <caption>{caption}</caption>
        <thead>
          <tr>
            <th scope="col">{columns[0]}</th>
            <th scope="col">{columns[1]}</th>
          </tr>
        </thead>
        <tbody>
          {bars.map((b) => (
            <tr key={b.key}>
              <th scope="row">{b.label}</th>
              <td>{b.detail}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  );
}
