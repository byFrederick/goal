import { type ReactNode, useId } from 'react';
import type { Board, BoardLane, BoardStation } from '../../api/types';
import { formatAge, formatPercent, humanize } from '../../shared/format';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { StationNode, type StationNodeTone } from '../../ui/station-node';

// The andon panel, "The line, now" (MASTER.md, Components): the factory's stations from intake to
// done, each a StationNode on a cobalt track, with what waits, its agent slots and its busy share.
// A phone stacks the stations down a track at the left; from 768 px they run across in a row.
export function LinePanel({
  board,
  constraint,
  doneThisWeek,
  className,
}: {
  board: Board;
  /** The constraint to show: the constraint report's, else the board's. */
  constraint: string | null;
  /** The items finished this week, from the 7-day overview; null while it loads. */
  doneThisWeek: number | null;
  className?: string;
}) {
  const headingId = useId();
  const { line, stations } = board;
  // The drum is the station the buffer feeds; it only means something under drum-buffer-rope.
  const drum = line.release.policy === 'drum-buffer-rope' ? line.release.drum : null;

  return (
    <section
      aria-labelledby={headingId}
      className={cn(
        'flex flex-col gap-4 rounded-andon bg-andon px-4 pt-4 pb-3 text-white md:px-6 md:pt-5 md:pb-4',
        className,
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
        <h2 id={headingId} className="m-0 text-heading text-white">
          The line, now
        </h2>
        <ul
          aria-label="Legend"
          className="m-0 flex list-none flex-wrap gap-x-4 gap-y-1 p-0 text-secondary text-andon-text"
        >
          <Legend swatch="bg-run-soft">Agent working</Legend>
          <Legend swatch="bg-you">Waiting on you</Legend>
          <Legend swatch="border border-andon-text">Free</Legend>
        </ul>
      </div>

      {stations.length === 0 ? (
        <p className="m-0 text-secondary text-andon-text">This factory has no stations yet.</p>
      ) : (
        <div className="relative">
          <span aria-hidden="true" className="absolute inset-x-[10%] top-6 hidden h-1 rounded-full bg-run md:block" />
          <span aria-hidden="true" className="absolute top-0 bottom-0 left-6 w-1 rounded-full bg-run md:hidden" />
          <ol
            aria-label="Stations from intake to done"
            className="relative flex flex-col gap-4 md:grid md:auto-cols-fr md:grid-flow-col"
          >
            {stations.map((s) => {
              const tone = toneOf(s, constraint);
              const busy = busyOf(s);
              return (
                <li
                  key={s.id}
                  className="relative flex items-center gap-3 md:flex-col md:items-center md:gap-2 md:text-center"
                >
                  <StationNode tone={tone} count={tone === 'done' ? undefined : s.waiting + s.in_progress} />
                  <div className="flex min-w-0 flex-1 flex-col items-start gap-1 md:flex-none md:items-center">
                    <span className="flex flex-wrap items-center gap-2 text-body font-semibold text-white">
                      {humanize(s.id)}
                      {tone === 'constraint' ? <Chip tone="constraint">Constraint</Chip> : null}
                    </span>
                    {waitsOf(s, drum, doneThisWeek).map((line) => (
                      <span key={String(line)} className="text-secondary text-andon-text">
                        {line}
                      </span>
                    ))}
                    <Slots station={s} />
                    {busy ? <span className="text-meta text-andon-text tabular-nums">{busy}</span> : null}
                  </div>
                </li>
              );
            })}
          </ol>
        </div>
      )}

      <footer className="flex flex-wrap items-center gap-x-7 gap-y-3 border-t border-andon-line pt-4 text-secondary text-andon-text">
        <span>
          In progress{' '}
          <strong className="font-semibold text-white">
            {line.wip.limit !== null ? `${line.wip.in_progress} of ${line.wip.limit}` : line.wip.in_progress}
          </strong>
          , ready <strong className="font-semibold text-white">{line.release.in_buffer}</strong>
        </span>
        {drum ? (
          <span>
            New work enters <strong className="font-semibold text-white">when {humanize(drum)} has room</strong>
          </span>
        ) : null}
        {line.expedite ? (
          <span>
            Expedite lane{' '}
            <strong className="font-semibold text-white">
              {line.expedite.limit !== null
                ? `${line.expedite.in_use} of ${line.expedite.limit}`
                : `${line.expedite.in_use} in use`}
            </strong>
          </span>
        ) : null}
        <span className="md:ml-auto">
          Andon{' '}
          <strong className="font-semibold text-white">{line.andon ? `pulled: ${line.andon.reason}` : 'clear'}</strong>
        </span>
      </footer>
    </section>
  );
}

function Legend({ swatch, children }: { swatch: string; children: ReactNode }) {
  return (
    <li className="inline-flex items-center gap-2">
      <span aria-hidden="true" className={cn('size-3.5 rounded-chip', swatch)} />
      {children}
    </li>
  );
}

/** The constraint rings in yellow, Done gets its check, and every other station is plain. */
export function toneOf(s: BoardStation, constraint: string | null): StationNodeTone {
  if (s.kind === 'done') return 'done';
  return s.id === constraint ? 'constraint' : 'plain';
}

/** What waits at the station, in the words the panel uses; one line each. */
function waitsOf(s: BoardStation, drum: string | null, doneThisWeek: number | null): ReactNode[] {
  if (s.kind === 'done') {
    return [
      s.done_today !== null ? `${s.done_today} done today` : null,
      doneThisWeek !== null ? `${doneThisWeek} this week` : null,
    ].filter((line) => line !== null);
  }
  if (s.kind === 'queue') {
    return [
      `${s.waiting} waiting`,
      s.oldest_waiting_seconds !== null ? `Oldest ${formatAge(s.oldest_waiting_seconds)}` : null,
    ].filter((line) => line !== null);
  }
  if (s.kind === 'human') return [`${s.in_progress} waiting on you`];
  if (s.id === drum) return [`${s.waiting} ready in buffer`];
  return [`${s.in_progress} in progress`];
}

/** "Busy 94%" where the station has a busy share, else "Sends back 14%" where it has a send-back share. */
function busyOf(s: BoardStation): string | null {
  if (s.busy_ratio !== null) return `Busy ${formatPercent(s.busy_ratio)}`;
  if (s.send_back_ratio !== null) return `Sends back ${formatPercent(s.send_back_ratio)}`;
  return null;
}

/**
 * The station's agent slots: cobalt for the busy ones, violet for items waiting on a person, an
 * outline for the free. Violet is capped at the slots that are not busy, so the squares never
 * outnumber the slots. Named by the role, never by a copy.
 */
export function Slots({ station, compact = false }: { station: BoardStation; compact?: boolean }) {
  const agents: BoardLane[] = (station.lanes ?? []).filter((lane) => lane.kind === 'agent');
  const total = agents.find((lane) => lane.slots !== null)?.slots;
  const role = agents[0]?.role;
  if (!total || !role) return null;

  const busy = Math.min(
    agents.reduce((n, lane) => n + lane.busy, 0),
    total,
  );
  const waiting = Math.min(station.items.filter((it) => it.state === 'waiting_human').length, total - busy);
  const free = total - busy - waiting;
  const label = `${role}: ${busy} working, ${waiting} waiting on you, ${free} free`;

  return (
    <span role="img" aria-label={label} className={cn('flex flex-wrap gap-1 md:justify-center', compact && 'gap-0.5')}>
      {Array.from({ length: total }, (_, i) => (
        <span
          // biome-ignore lint/suspicious/noArrayIndexKey: the squares are fixed by the slot count and never reorder
          key={i}
          aria-hidden="true"
          className={cn(
            compact ? 'size-2 rounded-sm' : 'size-3.5 rounded-chip',
            i < busy ? 'bg-run-soft' : i < busy + waiting ? 'bg-you' : 'border border-andon-text',
          )}
        />
      ))}
    </span>
  );
}
