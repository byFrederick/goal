// A factory's line on Home (MASTER.md, Components: The line; screens/daily-work/home.jpg): one compact row of its
// stations from intake to done, each the count over its name with its agent slots under, joined to the next by a
// 1 px rule. Done shows what finished today with a green dot. Its board comes from boardQuery, so the Floor's
// invalidations keep it fresh. A stopped line mutes its counts (never opacity: MASTER.md).
// On a phone the line scrolls sideways inside its card. A failed board leaves only a note; a loading one shows its
// skeleton in the line's place.
import { useQuery } from '@tanstack/react-query';
import { Fragment } from 'react';
import { boardQuery } from '../../api/queries';
import type { Board } from '../../api/types';
import { cn } from '../../ui/cn';
import { type LineStation, Slots } from '../../ui/line';
import { NumberRoll } from '../../ui/number-roll';
import { Skeleton } from '../../ui/skeleton';
import { lineStations } from '../floor/line-stations';

/** The compact line's height, shared by the placeholder so the swap moves nothing. */
const lineHeight = 'h-12';

export function MiniLine({
  org,
  factory,
  name,
  constraint,
  stopped = false,
  className,
}: {
  org: string;
  /** The factory's id, which the board's route takes. */
  factory: string;
  /** The factory's name, for the line's label. */
  name: string;
  constraint: string | null;
  /** The andon is pulled: the counts turn muted; opacity would fail text contrast. */
  stopped?: boolean | undefined;
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
  return (
    // Skeleton is a grid whose one track sizes to its content, so the line's w-max list would widen the card
    // and the page. A minmax(0,1fr) track keeps the track at the card's width and the section scrolls inside it.
    <Skeleton
      ready={board !== undefined}
      className={cn('grid-cols-[minmax(0,1fr)]', className)}
      placeholder={<MiniLineShape />}
    >
      {board ? <HomeLine board={board} name={name} constraint={constraint} stopped={stopped} /> : null}
    </Skeleton>
  );
}

/** The skeleton of the line: three stations of the compact row's height. */
export function MiniLineShape() {
  return (
    <span className={cn('flex items-center gap-3', lineHeight)}>
      {['a', 'b', 'c'].map((k) => (
        <span key={k} className="h-full w-16 rounded-control bg-hover" />
      ))}
    </span>
  );
}

function HomeLine({
  board,
  name,
  constraint,
  stopped,
}: {
  board: Board;
  name: string;
  constraint: string | null;
  stopped: boolean;
}) {
  const stations = lineStations(board, constraint, null);
  return (
    <section
      aria-label={`${name} line`}
      // biome-ignore lint/a11y/noNoninteractiveTabindex: a scrolling region must be reachable by keyboard (axe scrollable-region-focusable).
      tabIndex={0}
      className="min-w-0 overflow-x-auto focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run"
    >
      <ol className="m-0 flex w-max list-none items-start gap-2 p-0">
        {stations.map((s, i) => (
          <Fragment key={s.id}>
            {i > 0 ? <li aria-hidden="true" className="mt-3 h-px w-5 shrink-0 bg-rule" /> : null}
            <Station station={s} stopped={stopped} />
          </Fragment>
        ))}
      </ol>
    </section>
  );
}

function Station({ station: s, stopped }: { station: LineStation; stopped: boolean }) {
  return (
    <li className="flex min-w-12 flex-col items-start gap-1">
      <span
        className={cn(
          'flex items-center gap-1 text-heading font-semibold tabular-nums transition-colors duration-(--duration-fast) ease-(--ease-out) motion-reduce:transition-none',
          stopped && 'text-muted',
        )}
      >
        {s.done ? <span aria-hidden="true" className="size-2 rounded-full bg-ok" /> : null}
        <NumberRoll value={s.count} />
      </span>
      <span className="text-meta whitespace-nowrap text-muted">{s.done ? 'Done today' : s.name}</span>
      {s.slots ? <Slots slots={s.slots} label={s.slotsLabel} /> : null}
    </li>
  );
}
