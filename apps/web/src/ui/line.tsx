import { ChevronRight } from 'lucide-react';
import { type ReactNode, useEffect, useState } from 'react';
import { cn } from './cn';
import { NumberRoll } from './number-roll';

// The line (MASTER.md, Components: The line): a plain section on the page, with no dark panel. Each station is a
// column: its name, the count at 24 px, a 4 px load bar (ink when busy, amber for the constraint), its agent slots
// as 8 px squares, and what waits in muted text. Stations are joined by a 1 px rule with a chevron. The constraint
// gets a yellow marker and the word "Constraint". While the andon is pulled, a red-tint strip slides down with the
// reason, who pulled it and Clear, and each station dims by opacity (MOTION.md, Andon pulled). The strip is mounted
// only while open or closing, on the .motion-panel and .motion-andon transition keyed on data-state; it keeps its
// last text while it slides away and unmounts when the exit ends. A line that loads stopped shows it without the
// entrance.

export interface LineSlots {
  total: number;
  busy: number;
  waiting: number;
}

export interface LineStation {
  id: string;
  name: string;
  /** The figure at the station: items waiting and in progress, or the finished count for Done. */
  count: number;
  /** Load from 0 to 1 for the bar; null leaves the bar out. */
  load: number | null;
  /** This station limits the whole line: the amber bar and the "Constraint" word. */
  constraint?: boolean;
  /** The Done station: its bar stays out and its count is what finished. */
  done?: boolean;
  /** What waits here, one line each, muted. */
  waits: readonly string[];
  /** The agent slots, when the station has agents. */
  slots: LineSlots | null;
  /** Named by the role the slots belong to, for a screen reader: "backend-engineer: 2 working, 1 free". */
  slotsLabel?: string | undefined;
  /** The busy or send-back share in words: "Busy 94%". */
  share?: string | null;
}

export interface LineAndon {
  reason: string;
  /** Who pulled the cord, in words: "the engine" or "a person". */
  pulledBy: string;
}

export function Line({
  title,
  label,
  stations,
  andon = null,
  dimmed,
  clear,
  footer,
  legend = false,
  compact = false,
  className,
}: {
  /** The section's heading. Omit it when the page already names the line. */
  title?: string;
  /** The section's name for a screen reader, when there is no heading. Defaults to the heading. */
  label?: string;
  stations: readonly LineStation[];
  /** The open andon pull, or null while the line runs. The strip slides in and out with it. */
  andon?: LineAndon | null;
  /** Dim the stations without a strip (a stopped line on Home). Defaults to dimmed while the andon is pulled. */
  dimmed?: boolean;
  /** The control that clears the andon, shown in the strip. */
  clear?: ReactNode;
  footer?: ReactNode;
  legend?: boolean;
  /** Smaller figures and no heading, for a line inside a row. */
  compact?: boolean;
  className?: string;
}) {
  const stopped = andon !== null;
  const isDimmed = dimmed ?? stopped;

  return (
    <section
      aria-label={label ?? title ?? 'The line'}
      className={cn('flex min-w-0 flex-col gap-3 text-ink', className)}
    >
      {title || legend ? (
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
          {title ? <h2 className="m-0 text-heading">{title}</h2> : <span />}
          {legend ? <Legend /> : null}
        </div>
      ) : null}

      <AndonStrip andon={andon} clear={clear} />

      {stations.length === 0 ? (
        <p className="m-0 text-secondary text-muted">This factory has no stations yet.</p>
      ) : (
        <ol
          aria-label="Stations from intake to done"
          className="m-0 flex list-none flex-col gap-4 p-0 md:flex-row md:gap-0"
        >
          {stations.map((s, i) => (
            <StationItem key={s.id} first={i === 0} station={s} compact={compact} dimmed={isDimmed} />
          ))}
        </ol>
      )}

      {footer ? (
        <footer className="flex flex-wrap items-center gap-x-7 gap-y-3 border-t border-line pt-3 text-secondary text-muted">
          {footer}
        </footer>
      ) : null}
    </section>
  );
}

const DIM =
  'transition-opacity duration-(--duration-fast) ease-(--ease-out) motion-reduce:transition-none data-[dimmed=true]:opacity-40';

/** One station, and the connector before it from the second station on. */
function StationItem({
  first,
  station,
  compact,
  dimmed,
}: {
  first: boolean;
  station: LineStation;
  compact: boolean;
  dimmed: boolean;
}) {
  const dim = dimmed ? 'true' : undefined;
  return (
    <>
      {first ? null : (
        <li aria-hidden="true" data-dimmed={dim} className={cn('hidden w-6 shrink-0 items-start pt-7 md:flex', DIM)}>
          <span className="flex w-full items-center">
            <span className="h-px flex-1 bg-rule" />
            <ChevronRight size={12} strokeWidth={1.75} className="-ml-px text-muted" />
          </span>
        </li>
      )}
      <li data-dimmed={dim} className={cn('flex min-w-0 flex-1 flex-col gap-2', DIM)}>
        <StationColumn station={station} compact={compact} />
      </li>
    </>
  );
}

function StationColumn({ station: s, compact }: { station: LineStation; compact: boolean }) {
  const barClass = s.constraint ? 'bg-amber' : 'bg-ink';
  return (
    <>
      <span className="flex flex-wrap items-center gap-2">
        <span className="text-secondary font-medium text-ink">{s.name}</span>
        {s.constraint ? (
          <span className="flex items-center gap-1 text-meta text-amber-ink">
            <span aria-hidden="true" className="size-1.5 rounded-full bg-amber" />
            Constraint
          </span>
        ) : null}
      </span>
      <NumberRoll value={s.count} className={cn(compact ? 'text-body font-medium' : 'text-kpi')} />
      {s.load !== null ? (
        <span aria-hidden="true" className="block h-1 w-full overflow-hidden rounded-full bg-hover">
          <span
            className={cn(
              'block h-full origin-left transition-transform duration-(--duration-fast) ease-(--ease-out)',
              barClass,
            )}
            style={{ transform: `scaleX(${clamp(s.load)})` }}
          />
        </span>
      ) : null}
      {s.slots ? <Slots slots={s.slots} label={s.slotsLabel} /> : null}
      {s.waits.map((line) => (
        <span key={line} className="text-meta text-muted tabular-nums">
          {line}
        </span>
      ))}
      {s.share ? <span className="text-meta text-muted tabular-nums">{s.share}</span> : null}
    </>
  );
}

/** The agent slots: ink for working, orange for waiting on a person, an outline for free. Never colour alone: the label says it. */
export function Slots({ slots, label }: { slots: LineSlots; label?: string | undefined }) {
  const { total, busy, waiting } = slots;
  const free = total - busy - waiting;
  const text = label ?? `${busy} working, ${waiting} waiting on you, ${free} free`;
  return (
    <span role="img" aria-label={text} className="flex flex-wrap gap-1">
      {Array.from({ length: total }, (_, i) => (
        <span
          // biome-ignore lint/suspicious/noArrayIndexKey: the squares are fixed by the slot count and never reorder
          key={i}
          aria-hidden="true"
          className={cn(
            'size-2 rounded-chip',
            i < busy ? 'bg-ink' : i < busy + waiting ? 'bg-you' : 'border border-rule',
          )}
        />
      ))}
    </span>
  );
}

function Legend() {
  return (
    <ul aria-label="Legend" className="m-0 flex list-none flex-wrap gap-x-4 gap-y-1 p-0 text-meta text-muted">
      <LegendItem swatch="bg-ink">Working</LegendItem>
      <LegendItem swatch="bg-you">Waiting on you</LegendItem>
      <LegendItem swatch="border border-rule">Free</LegendItem>
    </ul>
  );
}

function LegendItem({ swatch, children }: { swatch: string; children: ReactNode }) {
  return (
    <li className="inline-flex items-center gap-1">
      <span aria-hidden="true" className={cn('size-2 rounded-chip', swatch)} />
      {children}
    </li>
  );
}

/**
 * The red strip under the heading. It is mounted while the andon is pulled and while it plays its exit: a closed
 * strip is hidden from assistive technology and inert, keeps the last pull's words, and unmounts when its close ends.
 * A line that loads stopped marks the strip data-initial, which turns off the entrance.
 */
function AndonStrip({ andon, clear }: { andon: LineAndon | null; clear: ReactNode }) {
  const [last, setLast] = useState(andon);
  const [gone, setGone] = useState(andon === null);
  const [initial, setInitial] = useState(andon !== null);
  const open = andon !== null;
  if (andon && andon !== last) setLast(andon);
  if (open && gone) setGone(false);
  if (!open && initial) setInitial(false);
  const shown = andon ?? last;
  const closing = !open && !gone;

  // Radix-style safety net: if the exit's animationend never arrives, the strip still goes.
  useEffect(() => {
    if (!closing) return;
    const t = setTimeout(() => setGone(true), 600);
    return () => clearTimeout(t);
  }, [closing]);

  if (gone && !open) return null;

  return (
    <div
      role={open ? 'status' : undefined}
      data-state={open ? 'open' : 'closed'}
      data-initial={initial ? '' : undefined}
      aria-hidden={open ? undefined : true}
      inert={!open}
      onAnimationEnd={(e) => {
        if (!open && e.target === e.currentTarget) setGone(true);
      }}
      className="motion-panel motion-andon flex flex-wrap items-center gap-x-4 gap-y-2 rounded-control bg-rework-tint px-4 py-3 text-ink"
    >
      <span className="font-medium text-rework-ink">Line stopped</span>
      <span className="min-w-0 flex-1">{shown?.reason}</span>
      <span className="text-secondary text-muted">Pulled by {shown?.pulledBy}</span>
      {clear}
    </div>
  );
}

function clamp(n: number): number {
  return Math.min(1, Math.max(0, n));
}
