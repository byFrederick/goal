import { useNavigate } from '@tanstack/react-router';
import type { Board, Factory, Metrics } from '../../api/types';
import { usePhone } from '../../app/phone-chrome';
import { AndonButton, LineStatus } from '../../shared/andon';
import { NewItemButton } from '../../shared/new-item';
import { Chip } from '../../ui/chip';
import { SegmentedControl } from '../../ui/segmented-control';
import { daysIn, type FloorSearch, lastDaysSearch, rangeLabel, windowOf } from './range';

/** The Floor's route: its search (the range, the type and the touches grouping) is read from it. */
export const floorRoute = '/o/$org/f/$factory/';

/** The windows of the segmented control, as the value each one writes. */
type Window = '1' | '7' | '30';
const windowOptions = [
  { value: '1', label: 'Today' },
  { value: '7', label: '7 days' },
  { value: '30', label: '30 days' },
] as const satisfies readonly { value: Window; label: string }[];

/** The touches grouping of the search, kept when the window changes. */
const keepGrouping = ({ touches }: FloorSearch) => (touches ? { touches } : {});

/** The window control: it writes ?from&to (the work type and the touches grouping are kept); a custom range shows none pressed. */
function WindowControl({ metrics }: { metrics: Metrics }) {
  const navigate = useNavigate({ from: floorRoute });
  const type = metrics.type ?? undefined;
  const shown = windowOf(metrics.from, metrics.to);
  return (
    <SegmentedControl<Window>
      label="Time window"
      options={windowOptions}
      value={shown === null ? undefined : (String(shown) as Window)}
      onChange={(next) =>
        navigate({
          search: (prev) => ({ ...lastDaysSearch(Number(next), type), ...keepGrouping(prev) }),
          resetScroll: false,
        })
      }
    />
  );
}

/**
 * The top bar's actions on the Floor (floor.html, .tact): the time window, Pull andon (Clear the andon
 * while the cord is pulled) and New item. A phone keeps the window in the page body (FloorHeader).
 */
export function FloorTopBarActions({ factory, board, metrics }: { factory: Factory; board: Board; metrics: Metrics }) {
  const phone = usePhone();
  return (
    <div className="flex min-w-0 items-center justify-end gap-2">
      {phone ? null : <WindowControl metrics={metrics} />}
      <AndonButton factory={factory} andon={board.line.andon} />
      <NewItemButton factory={factory} />
    </div>
  );
}

/** The Floor's header: the title, the line's status and the range it shows. The actions are in the top bar. */
export function FloorHeader({ board, metrics }: { board: Board; metrics: Metrics }) {
  const phone = usePhone();
  const days = daysIn(metrics.from, metrics.to);
  const before = days === 1 ? 'the day before' : `the ${days} days before`;
  const andon = board.line.andon;

  return (
    <header className="flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <h1 className="m-0 text-page-title tracking-display">Floor</h1>
        {andon ? (
          <LineStatus andon={andon} />
        ) : (
          <Chip tone="ok" size="lg" dot>
            Line running
          </Chip>
        )}
        <p className="m-0 text-secondary text-muted tabular-nums">
          {`${rangeLabel(metrics.from, metrics.to)}, compared with ${before}`}
        </p>
      </div>
      {phone ? <WindowControl metrics={metrics} /> : null}
    </header>
  );
}
