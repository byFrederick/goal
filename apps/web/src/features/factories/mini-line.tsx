// A factory's line on Home (ADR 0016, wave 2): its stations from intake to done in the andon panel's
// navy, one compact StationNode each with what waits and its slots, and Done today. The stations run
// across in a row; on a phone the row scrolls inside its own focusable box. Its board comes from
// boardQuery, so the Floor's invalidations keep it fresh; a failed board leaves only this note.
import { useQuery } from '@tanstack/react-query';
import { boardQuery } from '../../api/queries';
import type { Board, BoardStation } from '../../api/types';
import { humanize, plural } from '../../shared/format';
import { cn } from '../../ui/cn';
import { StationNode, type StationNodeTone } from '../../ui/station-node';
import { Slots, toneOf } from '../floor/line-panel';

export function MiniLine({
  org,
  factory,
  name,
  constraint,
  className,
}: {
  org: string;
  /** The factory's id, which the board's route takes. */
  factory: string;
  /** The factory's name, for the box's label. */
  name: string;
  constraint: string | null;
  className?: string | undefined;
}) {
  const { data: board, isError, refetch } = useQuery(boardQuery(org, factory));

  if (isError) {
    return (
      <p className={cn('m-0 text-meta text-muted', className)}>
        Line not available.{' '}
        <button type="button" className="underline" onClick={() => refetch()}>
          Retry
        </button>
      </p>
    );
  }
  if (!board) {
    return (
      <span aria-busy="true" className={cn('block h-18 rounded-control bg-andon', className)}>
        <span className="sr-only">Loading the line of {name}</span>
      </span>
    );
  }
  return <Line board={board} name={name} constraint={constraint} className={className} />;
}

function Line({
  board,
  name,
  constraint,
  className,
}: {
  board: Board;
  name: string;
  constraint: string | null;
  className?: string | undefined;
}) {
  const { stations } = board;
  return (
    <section
      aria-label={`${name} line`}
      // biome-ignore lint/a11y/noNoninteractiveTabindex: a labelled scroll box is focusable, so keyboard users can scroll a long line
      tabIndex={0}
      className={cn(
        'min-w-0 overflow-x-auto rounded-control bg-andon px-3 py-3 text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run',
        className,
      )}
    >
      <div className="relative w-max">
        <span aria-hidden="true" className="absolute top-3 right-8 left-8 h-px rounded-full bg-run" />
        <ol aria-label={`${name}, stations from intake to done`} className="relative m-0 flex list-none gap-5 p-0">
          {stations.map((s) => (
            <Station key={s.id} station={s} tone={toneOf(s, constraint)} />
          ))}
        </ol>
      </div>
    </section>
  );
}

function Station({ station: s, tone }: { station: BoardStation; tone: StationNodeTone }) {
  const done = s.kind === 'done';
  return (
    <li className="relative flex min-w-16 flex-col items-center gap-1 text-center">
      <StationNode size="compact" tone={tone} count={done ? undefined : s.waiting + s.in_progress} />
      <span className="text-meta whitespace-nowrap text-andon-text">{done ? 'Done today' : humanize(s.id)}</span>
      {done ? (
        <span className="text-meta whitespace-nowrap text-andon-text tabular-nums">
          {s.done_today !== null ? plural(s.done_today, 'item') : 'No count'}
        </span>
      ) : (
        <Slots station={s} compact />
      )}
    </li>
  );
}
