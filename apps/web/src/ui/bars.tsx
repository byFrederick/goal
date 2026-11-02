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
 * Vertical bars on one baseline, oldest first: ink for the latest, `rule` for the earlier ones, each on a `hover`
 * track; a zero is an empty track. A dashed `muted` line marks the target; its label sits above the chart, clear of the bars. The scale tops out at the
 * target or the tallest bar. Hovering a bar shows its label and detail; the chart is an image
 * named by `summary`, and a visually hidden table (`caption`) carries every value for screen
 * readers. The fill grows from the baseline with a scale transform, and not at all under prefers-reduced-motion.
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
        <span aria-hidden="true" className="mb-1 self-end text-[13px] text-muted tabular-nums">
          {targetLabel}
        </span>
      ) : null}
      <div role="img" aria-label={summary} className="relative flex min-h-[140px] flex-1 items-end gap-1.5 md:gap-2">
        {target ? (
          <span
            aria-hidden="true"
            className="pointer-events-none absolute inset-x-0 z-[1] border-t-[1.5px] border-dashed border-muted"
            style={{ bottom: `${pct(target)}%` }}
          />
        ) : null}
        {bars.map((b, i) => {
          const latest = i === bars.length - 1;
          return (
            <div key={b.key} className="group relative flex h-full min-w-0 flex-1 items-end">
              <div className="relative h-full w-full overflow-hidden rounded-chip bg-hover">
                <div
                  className={cn(
                    'absolute inset-0 origin-bottom transition-transform duration-(--duration-fast) ease-out motion-reduce:transition-none',
                    latest ? 'bg-ink' : 'bg-rule',
                  )}
                  style={{ transform: `scaleY(${pct(b.value) / 100})` }}
                />
              </div>
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
