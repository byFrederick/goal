// "The line, now" on the Floor (MASTER.md, Components: The line): the factory's stations from intake to done, in
// the page's own colours, with the andon strip above them while the cord is pulled. The figures under it are the
// Board's; the stations come from lineStations, the same mapping Home's line uses.
import type { Board, Factory } from '../../api/types';
import { AndonButton } from '../../shared/andon';
import { humanize } from '../../shared/format';
import { Line, type LineAndon } from '../../ui/line';
import { lineStations } from './line-stations';

export function LineNow({
  factory,
  board,
  constraint,
  doneThisWeek,
}: {
  factory: Factory;
  board: Board;
  /** The constraint to show: the constraint report's, else the board's. */
  constraint: string | null;
  /** The items finished this week, from the 7-day overview; null while it loads. */
  doneThisWeek: number | null;
}) {
  const { line } = board;
  // The drum is the station the buffer feeds; it only means something under drum-buffer-rope.
  const drum = line.release.policy === 'drum-buffer-rope' ? line.release.drum : null;
  const andon: LineAndon | null = line.andon
    ? { reason: line.andon.reason, pulledBy: pulledBy(line.andon.pulled_by) }
    : null;

  return (
    <Line
      title="The line, now"
      legend
      stations={lineStations(board, constraint, doneThisWeek)}
      andon={andon}
      clear={line.andon ? <AndonButton factory={factory} andon={line.andon} /> : null}
      footer={
        <>
          <span>
            In progress{' '}
            <strong className="font-medium text-ink">
              {line.wip.limit !== null ? `${line.wip.in_progress} of ${line.wip.limit}` : line.wip.in_progress}
            </strong>
            , ready <strong className="font-medium text-ink">{line.release.in_buffer}</strong>
          </span>
          {drum ? (
            <span>
              New work enters <strong className="font-medium text-ink">when {humanize(drum)} has room</strong>
            </span>
          ) : null}
          {line.expedite ? (
            <span>
              Expedite lane{' '}
              <strong className="font-medium text-ink">
                {line.expedite.limit !== null
                  ? `${line.expedite.in_use} of ${line.expedite.limit}`
                  : `${line.expedite.in_use} in use`}
              </strong>
            </span>
          ) : null}
          <span className="md:ml-auto">
            Andon <strong className="font-medium text-ink">{line.andon ? 'pulled' : 'clear'}</strong>
          </span>
        </>
      }
    />
  );
}

/** Who pulled the cord, in words: the ref is "system:engine" for goald's own pulls, and a person's otherwise. */
export function pulledBy(ref: string): string {
  return ref.startsWith('system:') ? 'the engine' : 'a person';
}
