import { cn } from './cn';
import { NumberRoll } from './number-roll';

// The stat strip (MASTER.md, Components: Stat strip; MOTION.md, A number changes). Up to four figures in one
// row, separated by 1 px rules, with no boxes. Each has a muted label, its figure at 24 px with the unit beside
// it, and one meta line: the target, and the change against the range before, in ok-ink or rework-ink. A 2 px
// bar under the figure shows progress to the target, and only when there is one. A figure with no value shows
// an em dash that a screen reader hears as "No data yet"; a strip never shows a blank.

/** The most figures a strip shows. A fifth is dropped, not squeezed in. */
export const maxStats = 4;

export interface Stat {
  label: string;
  /** The figure. Null while there is nothing to measure: the strip shows an em dash. */
  value: number | string | null;
  /** The unit after the figure, at 13 px in muted: "a day", "%", "h". */
  unit?: string | undefined;
  /** The target, as the meta text's first part: "Target 10". */
  target?: string | undefined;
  /** The change against the range before, signed and already formatted: "−0.6". */
  delta?: { text: string; good: boolean } | undefined;
  /** Progress to the target from 0 to 1, drawn as the 2 px bar. Give it only with a target. */
  progress?: number | undefined;
  /** The meta text when there is no target and no delta: "No target set". */
  note?: string | undefined;
  /** Colours the figure by meaning: rework-ink for failures, you-ink for what waits on a person. */
  tone?: 'rework' | 'you' | undefined;
}

/** Columns from `md` up: one per figure. Below `md` the strip is two columns, so four figures sit two by two. */
const mdColumns: Record<number, string> = {
  1: 'md:grid-cols-1',
  2: 'md:grid-cols-2',
  3: 'md:grid-cols-3',
  4: 'md:grid-cols-4',
};

/** Columns from `2xl` up, for a narrow strip: three or four figures. */
const twoXlColumns: Record<number, string> = {
  3: '2xl:grid-cols-3',
  4: '2xl:grid-cols-4',
};

export function StatStrip({
  stats,
  label,
  className,
  narrow = false,
}: {
  stats: readonly Stat[];
  label: string;
  className?: string;
  /** For a strip in a column too narrow for four figures at `md`: it stays two by two until `2xl`. */
  narrow?: boolean;
}) {
  const shown = stats.slice(0, maxStats);
  const columns = narrow && shown.length > 2 ? twoXlColumns[shown.length] : mdColumns[shown.length];
  const rule = narrow ? '2xl:border-l 2xl:border-line 2xl:pl-5' : 'md:border-l md:border-line md:pl-5';
  return (
    <section aria-label={label} className={cn('grid grid-cols-2 gap-y-4', columns, className)}>
      {shown.map((stat, i) => (
        <div
          key={stat.label}
          className={cn(
            'flex min-w-0 flex-col gap-1 pr-5',
            i % 2 === 1 ? 'border-l border-line pl-5' : i > 0 ? 'pl-0' : '',
            i > 0 && rule,
          )}
        >
          <span className="text-meta text-muted">{stat.label}</span>
          <StatFigure stat={stat} />
          <StatMeta stat={stat} />
        </div>
      ))}
    </section>
  );
}

function StatFigure({ stat }: { stat: Stat }) {
  if (stat.value === null) {
    return (
      <span className="flex min-w-0 flex-wrap items-baseline gap-x-1 break-words text-kpi tabular-nums">
        <span aria-hidden="true">—</span>
        <span className="sr-only">No data yet</span>
      </span>
    );
  }
  return (
    <span className="flex min-w-0 flex-wrap items-baseline gap-x-1 break-words">
      <NumberRoll
        value={stat.value}
        className={cn('text-kpi', stat.tone === 'rework' && 'text-rework-ink', stat.tone === 'you' && 'text-you-ink')}
      />
      {stat.unit ? <span className="text-secondary text-muted">{stat.unit}</span> : null}
    </span>
  );
}

function StatMeta({ stat }: { stat: Stat }) {
  const bar = stat.target !== undefined && stat.progress !== undefined;
  return (
    <>
      {bar ? (
        <span aria-hidden="true" className="block h-0.5 w-full overflow-hidden rounded-full bg-hover">
          <span
            className="block h-full origin-left bg-ink transition-transform duration-(--duration-fast) ease-(--ease-out)"
            style={{ transform: `scaleX(${clamp(stat.progress ?? 0)})` }}
          />
        </span>
      ) : null}
      <span className="flex flex-wrap items-baseline gap-x-2 text-meta text-muted tabular-nums">
        {stat.target ? <span>{stat.target}</span> : null}
        {stat.delta ? (
          <span className={stat.delta.good ? 'text-ok-ink' : 'text-rework-ink'}>{stat.delta.text}</span>
        ) : null}
        {!stat.target && !stat.delta && stat.note ? <span>{stat.note}</span> : null}
      </span>
    </>
  );
}

function clamp(n: number): number {
  return Math.min(1, Math.max(0, n));
}
