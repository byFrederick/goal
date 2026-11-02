// A factory's line on Home (MASTER.md, Components: The line): its stations from intake to done in the page's own
// colours, compact, with what waits and the agent slots. Its board comes from boardQuery, so the Floor's
// invalidations keep it fresh. A stopped line dims. A failed board leaves only this note; a loading one shows its
// skeleton in the line's place.
import { useQuery } from '@tanstack/react-query';
import { boardQuery } from '../../api/queries';
import type { Board } from '../../api/types';
import { cn } from '../../ui/cn';
import { Line } from '../../ui/line';
import { Skeleton } from '../../ui/skeleton';
import { lineStations } from '../floor/line-stations';

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
  /** The andon is pulled: the stations dim, as the picture's stopped line does. */
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
    <Skeleton
      ready={board !== undefined}
      className={className}
      placeholder={<span className="block h-20 rounded-control bg-hover" />}
    >
      {board ? <HomeLine board={board} name={name} constraint={constraint} stopped={stopped} /> : null}
    </Skeleton>
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
  return (
    <Line
      label={`${name} line`}
      compact
      dimmed={stopped}
      stations={lineStations(board, constraint, null)}
      className="min-w-0"
    />
  );
}
