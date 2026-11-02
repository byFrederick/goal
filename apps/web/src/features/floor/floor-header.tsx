import { useNavigate } from '@tanstack/react-router';
import { useMemo } from 'react';
import type { Board, Factory, Metrics } from '../../api/types';
import { usePhone } from '../../app/phone-chrome';
import { TopBarSlot } from '../../app/top-bar';
import { AndonButton, LineStatus } from '../../shared/andon';
import { NeedsYouLink } from '../../shared/needs-you-link';
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

/**
 * The Floor's header: the title, the line's status, the range it shows and the actions. The window
 * control writes ?from&to (the work type and the touches grouping are kept); a custom range shows
 * no window as pressed.
 */
export function FloorHeader({ factory, board, metrics }: { factory: Factory; board: Board; metrics: Metrics }) {
  const navigate = useNavigate({ from: floorRoute });
  const type = metrics.type ?? undefined;
  const shown = windowOf(metrics.from, metrics.to);
  const days = daysIn(metrics.from, metrics.to);
  const before = days === 1 ? 'the day before' : `the ${days} days before`;
  const andon = board.line.andon;
  // On a phone, Needs you, Pull andon and New item live in the top bar; the page keeps the status and the range.
  const phone = usePhone();
  const actions = useMemo(
    () =>
      phone ? (
        <div className="flex items-center gap-2">
          <NeedsYouLink factory={factory} />
          {/* While the cord is pulled, its strip under the line holds the Clear control instead. */}
          {andon ? null : <AndonButton factory={factory} andon={andon} />}
          <NewItemButton factory={factory} compact />
        </div>
      ) : undefined,
    [phone, factory, andon],
  );

  return (
    <>
      {phone ? <TopBarSlot actions={actions} /> : null}
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
        <div className="flex flex-wrap items-center gap-2">
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
          {phone ? null : (
            <>
              <NeedsYouLink factory={factory} />
              {/* While the cord is pulled, its strip under the line holds the Clear control instead. */}
              {andon ? null : <AndonButton factory={factory} andon={andon} />}
              <NewItemButton factory={factory} />
            </>
          )}
        </div>
      </header>
    </>
  );
}
