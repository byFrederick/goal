import type { ReactNode } from 'react';
import { cn } from './cn';
import { Hatch } from './hatch';

// A waterfall (bridge) from a target to an actual value (MASTER.md data marks: hatched gray
// target, hatched rust delay steps, solid teal actual; every value signed and labelled). Each
// step starts where the one before ended, so the steps add up from the target to the actual.

export interface BridgeInput {
  target: number;
  steps: readonly { key: string; value: number }[];
  actual: number;
}

export interface BridgeBar {
  key: string;
  kind: 'target' | 'step' | 'actual';
  /** In the input's unit. */
  from: number;
  to: number;
  /** In percent of the axis. */
  left: number;
  width: number;
  end: number;
}

export interface BridgeGeometry {
  domain: [number, number];
  ticks: number[];
  rows: BridgeBar[];
}

/** The nice tick step nearest (on a log scale) to `raw`: 1, 2, 2.5 or 5 times a power of ten. */
function niceStep(raw: number): number {
  if (!(raw > 0)) return 1;
  const power = 10 ** Math.floor(Math.log10(raw));
  const candidates = [1, 2, 2.5, 5, 10].map((m) => m * power);
  return candidates.reduce((best, c) => (Math.abs(Math.log(c / raw)) < Math.abs(Math.log(best / raw)) ? c : best));
}

const round = (n: number) => Math.round(n * 1e9) / 1e9;

/**
 * Where every bar of a bridge goes: an axis from half a tick below the target (never below 0) to
 * the first tick at or past the actual, ticks a nice step apart (about a third of the gap), the
 * target bar from the axis start, each step from the end of the one before, and the actual as a
 * marker ending at its value.
 */
export function bridgeGeometry({ target, steps, actual }: BridgeInput): BridgeGeometry {
  const step = niceStep((actual - target) / 3);
  const lo = Math.max(0, round(target - step / 2));
  const ticks = [lo];
  while ((ticks.at(-1) ?? lo) < actual) ticks.push(round(lo + ticks.length * step));
  const hi = ticks.at(-1) ?? lo + step;
  const pct = (v: number) => ((v - lo) / (hi - lo)) * 100;
  const bar = (key: string, kind: BridgeBar['kind'], from: number, to: number): BridgeBar => ({
    key,
    kind,
    from,
    to,
    left: pct(from),
    width: pct(to) - pct(from),
    end: pct(to),
  });

  const rows = [bar('target', 'target', lo, target)];
  let at = target;
  for (const s of steps) {
    rows.push(bar(s.key, 'step', at, at + s.value));
    at += s.value;
  }
  rows.push(bar('actual', 'actual', actual, actual));
  return { domain: [lo, hi], ticks, rows };
}

export interface BridgeRow {
  key: string;
  label: ReactNode;
  /** The signed value in words: "+2.1 h, 11 items". */
  text: string;
}

/**
 * The bridge chart: one row per bar (label and value above its track), dashed connectors
 * between steps and the axis ticks under it. An image named by `summary`; the visually hidden
 * table (`caption`, `columns`) carries the rows' labels and values for screen readers.
 */
export function Bridge({
  input,
  rows,
  formatTick,
  summary,
  caption,
  columns,
  className,
}: {
  input: BridgeInput;
  /** The target, each step, the actual: as many as the input has bars. */
  rows: readonly BridgeRow[];
  formatTick: (v: number) => string;
  summary: string;
  caption: string;
  columns: readonly [string, string];
  className?: string;
}) {
  const g = bridgeGeometry(input);
  return (
    <figure className={cn('m-0 flex flex-col', className)}>
      <div role="img" aria-label={summary}>
        <ol className="m-0 flex list-none flex-col gap-[22px] p-0">
          {g.rows.map((b, i) => {
            const row = rows[i];
            return (
              <li key={b.key} className="flex flex-col gap-2.5">
                <div className="flex justify-between gap-2.5 text-[15px]">
                  <span className="min-w-0">{row?.label}</span>
                  <span
                    className={cn('shrink-0 text-sm tabular-nums', b.kind === 'step' ? 'text-rework-ink' : 'text-ink')}
                  >
                    {row?.text}
                  </span>
                </div>
                <div className="relative h-5">
                  {i > 0 ? (
                    <span
                      className="absolute -top-[42px] h-[42px] border-l-[1.5px] border-dashed border-faint"
                      style={{ left: `${b.kind === 'actual' ? b.end : b.left}%` }}
                    />
                  ) : null}
                  {b.kind === 'actual' ? (
                    <div
                      className="absolute top-0 h-5 w-[3%] min-w-7 -translate-x-full rounded-[4px] bg-run"
                      style={{ left: `${b.end}%` }}
                    />
                  ) : (
                    <div
                      className="absolute top-0 h-5 min-w-1.5 overflow-hidden rounded-[3px]"
                      style={{ left: `${b.left}%`, width: `${Math.max(b.width, 1.8)}%` }}
                    >
                      <Hatch tone={b.kind === 'target' ? 'target' : 'rework'} />
                    </div>
                  )}
                </div>
              </li>
            );
          })}
        </ol>
        <div className="mt-3.5 flex justify-between text-xs text-muted tabular-nums">
          {g.ticks.map((t) => (
            <span key={t}>{formatTick(t)}</span>
          ))}
        </div>
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
          {rows.map((r) => (
            <tr key={r.key}>
              <th scope="row">{r.label}</th>
              <td>{r.text}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  );
}
