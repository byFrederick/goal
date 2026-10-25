// Human touches per finished item on the Floor (spec 4 §4.9, §10; canvas Main and Mobile-Floor):
// from 768 px a panel across the top (the headline against the range before, touchless and
// unplanned per item and the line stops people made, then where the touches happen by station,
// kind, reason or definition version); on a phone a compact card under Throughput. The rest of
// the Floor never waits for them: while they load, or when they fail, TouchesWaiting holds the place.
import { Link, useNavigate } from '@tanstack/react-router';
import { type ReactNode, useId } from 'react';
import type { TouchMetrics, TouchStats } from '../../api/types';
import { useFactoryParams } from '../../shared/factory';
import { formatPercent, plural } from '../../shared/format';
import {
  mostTouched,
  perItem,
  type TouchGrouping,
  type TouchRow,
  touchGroupings,
  touchRows,
} from '../../shared/touches';
import { Button } from '../../ui/button';
import { SectionCard, StatRow } from '../../ui/card';
import { cn } from '../../ui/cn';
import { Hatch } from '../../ui/hatch';
import { SegmentedControl } from '../../ui/segmented-control';
import { DeltaChip } from './delta-chip';
import { touchesDelta } from './deltas';
import { previousName, rangeLabel } from './range';

const numeral = 'm-0 font-normal leading-none tracking-numeral tabular-nums';
const shownBars = 4;

interface Props {
  touches: TouchMetrics;
  /** The same range just before, to compare with. */
  previous: TouchMetrics | undefined;
  className?: string;
}

/** The touches no person made: the finished items nobody touched, as a count. */
const touchless = (s: TouchStats) => Math.round(s.touchless_rate * s.finished);

/** What the headline says when nothing finished in the range. */
const noneFinished = (t: TouchMetrics) =>
  `No items finished ${rangeLabel(t.from, t.to)}. Touches count once an item is done.`;

export function TouchesPanel({ touches, previous, grouping, className }: Props & { grouping: TouchGrouping }) {
  const s = touches.stats;
  const headingId = useId();
  const delta = touchesDelta(s, previous?.stats);
  const cell = 'flex min-w-0 flex-col p-5';
  const rule = 'border-t border-line @[720px]:border-t-0';

  return (
    <section
      id="touches"
      aria-labelledby={headingId}
      className={cn(
        'hidden scroll-mt-4 rounded-card border border-line bg-card md:grid',
        '@[720px]:grid-cols-2 @[960px]:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.35fr)]',
        className,
      )}
    >
      <div className={cell}>
        <h2 id={headingId} className="m-0 text-lg font-medium">
          Human touches
        </h2>
        <p className="m-0 mt-1 text-sm leading-[1.45] text-muted">
          Any time a person acts on an item: approve, send back, move, retry, note. The goal is fewer.
        </p>
        {s.finished === 0 ? (
          <p className="m-0 mt-4 text-[15px] leading-normal">{noneFinished(touches)}</p>
        ) : (
          <>
            <p className={cn(numeral, 'mt-3.5 mb-2 text-[64px]')}>
              {perItem(s.per_item_mean)}
              <span className="ml-1.5 text-lg tracking-normal text-muted"> touches per item</span>
            </p>
            <p className="m-0 flex items-center gap-2.5 text-sm text-muted">
              {delta ? <DeltaChip delta={delta} /> : null}
              <span className="min-w-0">
                {delta ? `vs ${previousName(touches.from, touches.to)}. ` : ''}
                {plural(s.touches, 'touch', 'touches')} on {plural(s.finished, 'finished item')}
              </span>
            </p>
          </>
        )}
      </div>

      <dl className={cn(cell, rule, 'm-0 @[720px]:border-l')}>
        <Figure value={formatPercent(s.touchless_rate)}>
          Touchless: {touchless(s)} of {plural(s.finished, 'item')} finished with no human touch
        </Figure>
        <Figure value={perItem(s.unplanned_per_item)} unit="unplanned">
          per item, plus {perItem(s.planned_per_item)} planned: the approvals and merges the line requires
        </Figure>
        <Figure value={`${s.line_stops}`} unit={s.line_stops === 1 ? 'line stop' : 'line stops'}>
          Andon pulls people made {rangeLabel(touches.from, touches.to)}: the line's, not an item's
        </Figure>
      </dl>

      <div
        className={cn(
          cell,
          rule,
          '@[720px]:col-span-2 @[720px]:border-t @[960px]:col-span-1 @[960px]:border-t-0 @[960px]:border-l',
        )}
      >
        <WhereTouchesHappen stats={s} grouping={grouping} />
      </div>
    </section>
  );
}

/**
 * One figure of the panel's middle column: a large value with its words under it. The words are
 * the term and the value its definition; only the drawing puts the value first.
 */
function Figure({ value, unit, children }: { value: string; unit?: string; children: ReactNode }) {
  return (
    <div className="flex flex-col-reverse gap-0.5 border-t border-line py-3 first:border-t-0 first:pt-0 last:pb-0">
      <dt className="text-sm leading-[1.4] text-muted">{children}</dt>
      <dd className="m-0 text-[28px] leading-[1.1] tracking-display tabular-nums">
        {value}
        {unit ? <span className="ml-1 text-[15px] tracking-normal text-muted"> {unit}</span> : null}
      </dd>
    </div>
  );
}

function WhereTouchesHappen({ stats, grouping }: { stats: TouchStats; grouping: TouchGrouping }) {
  const navigate = useNavigate({ from: '/o/$org/f/$factory/' });
  const params = useFactoryParams();
  const headingId = useId();
  const rows = touchRows(stats, grouping);
  const shown = rows.slice(0, shownBars);
  const rest = rows.slice(shownBars);
  const max = Math.max(1, ...rows.map((r) => r.touches));

  return (
    <section aria-labelledby={headingId} className="flex min-w-0 flex-col">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <h3 id={headingId} className="m-0 text-[15px] font-medium">
          Where touches happen
        </h3>
        <SegmentedControl
          label="Group touches by"
          options={touchGroupings}
          value={grouping}
          onChange={(by) =>
            navigate({
              search: ({ touches: _, ...rest }) => (by === 'station' ? rest : { ...rest, touches: by }),
              replace: true,
              resetScroll: false,
            })
          }
        />
      </div>
      {rows.length === 0 ? (
        <p className="m-0 text-sm text-muted">No one touched the items finished in this range.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {shown.map((r) => (
            <TouchBar key={r.key} row={r} max={max} />
          ))}
        </ul>
      )}
      <p className="m-0 mt-1.5 text-[13px] text-muted">
        {rest.length > 0 ? `${rest.map((r) => `${r.label} ${r.touches}`).join(', ')}. ` : ''}
        <Link
          to="/o/$org/f/$factory/items"
          params={params}
          search={{ state: 'done' }}
          className="text-ink underline-offset-2 transition-colors duration-200 hover:text-run"
        >
          See the finished items
        </Link>
      </p>
    </section>
  );
}

function TouchBar({ row, max }: { row: TouchRow; max: number }) {
  return (
    <li className="grid grid-cols-[92px_minmax(0,1fr)_auto] items-center gap-x-2.5 py-1 text-sm">
      <span className="truncate" title={row.label}>
        {row.label}
      </span>
      <span
        role="img"
        aria-label={`${row.label}: ${plural(row.touches, 'touch', 'touches')}, ${perItem(row.perItem, 2)} per item`}
        className="block h-2.5 overflow-hidden rounded-[3px] bg-surface"
      >
        <span className="block h-full overflow-hidden rounded-[3px]" style={{ width: `${(row.touches / max) * 100}%` }}>
          <Hatch tone="run" />
        </span>
      </span>
      <span className="min-w-[2ch] text-right font-medium tabular-nums">{row.touches}</span>
      <small className="col-start-2 col-end-4 -mt-0.5 text-xs text-muted">{row.caption}</small>
    </li>
  );
}

/** Phones: the headline, touchless and unplanned per item, and where most touches happen. */
export function TouchesSummary({ touches, previous, className }: Props) {
  const s = touches.stats;
  const delta = touchesDelta(s, previous?.stats);
  const most = mostTouched(s);

  return (
    <SectionCard id="touches-summary" title="Human touches" className={cn('scroll-mt-4 gap-2.5 md:hidden', className)}>
      {s.finished === 0 ? (
        <p className="m-0 text-[15px] leading-normal">{noneFinished(touches)}</p>
      ) : (
        <>
          <p className={cn(numeral, 'text-[52px]')}>
            {perItem(s.per_item_mean)}
            <span className="ml-1 text-[17px] tracking-normal text-muted"> per item</span>
          </p>
          {delta ? (
            <p className="m-0 flex flex-wrap items-center gap-x-1.5 text-sm">
              <DeltaChip delta={delta} />
              <span className="text-muted">vs {previousName(touches.from, touches.to)}, fewer is better</span>
            </p>
          ) : null}
          <div className="flex flex-col">
            <StatRow label="Touchless items" className="[&>span:first-child]:text-ink">
              <strong className="font-medium">{formatPercent(s.touchless_rate)}</strong>
            </StatRow>
            <StatRow label="Unplanned per item" className="[&>span:first-child]:text-ink">
              <strong className="font-medium">{perItem(s.unplanned_per_item)}</strong>
            </StatRow>
          </div>
          {most ? <p className="m-0 text-[13px] leading-[1.45] text-muted">{most}</p> : null}
        </>
      )}
    </SectionCard>
  );
}

/** The touches' place while they load, or after they failed, with Try again. */
export function TouchesWaiting({
  failed,
  onRetry,
  className,
}: {
  failed: boolean;
  onRetry: () => void;
  className?: string;
}) {
  return (
    <SectionCard title="Human touches" className={cn('gap-2.5', className)}>
      {failed ? (
        <div role="alert" className="flex flex-col items-start gap-3">
          <p className="m-0 text-sm leading-normal text-rework-ink">Couldn’t load the human touches.</p>
          <Button variant="white" className="h-11 md:h-10" onClick={onRetry}>
            Try again
          </Button>
        </div>
      ) : (
        <p className="m-0 text-sm text-muted">Loading the human touches…</p>
      )}
    </SectionCard>
  );
}
