import { cn } from './cn';

export interface Bar {
  key: string;
  value: number;
  /** What the bar is: "Oct 10 to 16". */
  label: string;
  /** Its value in words: "1.4 a day, 10 finished". */
  detail: string;
}

/**
 * Vertical bars on one baseline, oldest first: every week a solid bar in the rule colour, the latest in
 * ink; a zero is a flat stub. A dashed ink line marks the target; its label sits above the chart, clear of
 * the bars. The scale tops out at the
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
      {target && targetLabel ? (
        <span aria-hidden="true" className="mb-1 self-end text-[13px] text-ink tabular-nums">
          {targetLabel}
        </span>
      ) : null}
      <div role="img" aria-label={summary} className="relative flex min-h-[140px] flex-1 items-end gap-1.5 md:gap-2">
        {target ? (
          <span
            aria-hidden="true"
            className="pointer-events-none absolute inset-x-0 z-[1] border-t-[1.5px] border-dashed border-ink"
            style={{ bottom: `${pct(target)}%` }}
          />
        ) : null}
        {bars.map((b, i) => {
          const latest = i === bars.length - 1;
          return (
            <div key={b.key} className="group relative flex h-full min-w-0 flex-1 items-end">
              {b.value > 0 ? (
                <div
                  className={cn(
                    'w-full rounded-t-[3px] rounded-b-[1px] transition-[height] duration-(--duration-fast) ease-out motion-reduce:transition-none',
                    latest ? 'bg-ink' : 'bg-rule',
                  )}
                  style={{ height: `${pct(b.value)}%` }}
                />
              ) : (
                <div className="h-0.5 w-full rounded-[1px] bg-rule" />
              )}
              <span
                aria-hidden="true"
                className={cn(
                  'pointer-events-none absolute bottom-full z-10 mb-1.5 hidden rounded-chip border border-rule bg-card px-2.5 py-1.5 text-[13px] whitespace-nowrap text-ink shadow-pop group-hover:block',
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
