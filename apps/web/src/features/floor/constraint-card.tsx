import { Link } from '@tanstack/react-router';
import { ChartNoAxesColumn, ChevronLeft, ChevronRight } from 'lucide-react';
import { useState } from 'react';
import type { Board, ConstraintReport } from '../../api/types';
import { useFactoryParams } from '../../shared/factory';
import { formatDay, formatPercent, humanize, plural } from '../../shared/format';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { addLabel, busiestSentence, busyPeriod, estimateSentence, headline, waitingSentence } from './constraint-words';

// Numbers are tabular (MASTER.md); names around them stay proportional.
const numeral = 'm-0 font-normal tracking-numeral leading-none tabular-nums';

const pagerButton =
  'inline-flex size-11 items-center justify-center rounded-control text-white/85 transition-colors duration-200 hover:bg-white/10 hover:text-white disabled:cursor-default disabled:opacity-40 disabled:hover:bg-transparent md:size-8';

/**
 * The dark card (Main and Mobile-Floor: the constraint): the busiest agent stations over the
 * range, one at a time ("1 of 3"), with how busy each was, the queue in front of it and what one
 * more slot would save, and a link to add one in the definition. When no station is loaded
 * enough to hold the line back (domain.ConstraintMinBusy, or work waiting there) it says "No
 * constraint right now" and names the busiest. Before any lease history it shows the snapshot of
 * the board and when busy shares will start.
 */
export function ConstraintCard({
  report,
  board,
  className,
}: {
  report: ConstraintReport;
  board: Board;
  className?: string;
}) {
  const [page, setPage] = useState(0);
  const cands = report.candidates;
  const index = Math.min(page, Math.max(cands.length - 1, 0));
  const c = cands[index];
  const period = busyPeriod(report);

  return (
    <section
      aria-labelledby="constraint-h"
      className={cn(
        'relative flex min-h-0 flex-col gap-2.5 overflow-hidden rounded-card bg-ink p-4 text-white md:min-h-[460px] md:gap-0 md:p-[22px]',
        className,
      )}
    >
      <span
        aria-hidden="true"
        className="pointer-events-none absolute -right-12 -bottom-16 size-56 rounded-full border-[1.5px] border-white/20"
      />
      <span
        aria-hidden="true"
        className="pointer-events-none absolute right-6 bottom-8 hidden h-28 w-11 rounded-l-control border-[1.5px] border-r-0 border-white/35 md:block"
      />
      {c && report.constraint === null ? (
        <NoConstraint detail={busiestSentence(c, period.long)} />
      ) : c ? (
        <>
          <div className="relative flex items-center justify-between gap-2">
            <Chip tone="constraint" className="h-7 px-2.5 text-[13px] font-semibold">
              <ChartNoAxesColumn size={14} strokeWidth={2} aria-hidden="true" />
              {report.source === 'detected' ? 'Constraint found' : 'Constraint'}
            </Chip>
            {cands.length > 1 ? (
              <nav aria-label="Candidates" className="flex items-center gap-1 text-[13px] text-white/85">
                <button
                  type="button"
                  aria-label="Previous candidate"
                  disabled={index === 0}
                  onClick={() => setPage(index - 1)}
                  className={pagerButton}
                >
                  <ChevronLeft size={16} strokeWidth={1.75} aria-hidden="true" />
                </button>
                <span aria-live="polite" className="tabular-nums">
                  {index + 1} of {cands.length}
                </span>
                <button
                  type="button"
                  aria-label="Next candidate"
                  disabled={index === cands.length - 1}
                  onClick={() => setPage(index + 1)}
                  className={pagerButton}
                >
                  <ChevronRight size={16} strokeWidth={1.75} aria-hidden="true" />
                </button>
              </nav>
            ) : null}
          </div>
          <p className="relative m-0 flex items-end gap-3 md:mt-7">
            <span className={cn(numeral, 'text-[44px] md:text-[58px]')}>{formatPercent(c.busy_ratio)}</span>
            <span className="pb-1 text-sm leading-snug text-white/85 md:pb-1.5">
              busy<span className="md:hidden">,</span> <br className="max-md:hidden" />
              <span className="md:hidden">{period.short}</span>
              <span className="max-md:hidden">{period.long}</span>
            </span>
          </p>
          <h2
            id="constraint-h"
            className="relative m-0 max-w-[18ch] text-xl leading-tight font-medium md:mt-5 md:text-2xl"
          >
            {headline(c, index, report.constraint)}
          </h2>
          <p className="relative m-0 max-w-[34ch] text-sm leading-normal text-white/85 md:mt-3 md:text-[15px]">
            <span className="max-md:hidden">{waitingSentence(c)} </span>
            {estimateSentence(c)}
          </p>
          <AddLink station={c.station} label={addLabel(c)} />
        </>
      ) : (
        <Snapshot report={report} board={board} />
      )}
    </section>
  );
}

/** Before busy shares can be measured: the board's snapshot constraint and when history starts. */
function Snapshot({ report, board }: { report: ConstraintReport; board: Board }) {
  const station = board.stations.find((s) => s.id === report.constraint);
  const history = report.coverage_since
    ? `Not enough history yet: busy shares start on ${formatDay(report.coverage_since)}.`
    : 'Not enough history yet: busy shares start with the first agent run.';

  if (!station) return <NoConstraint />;

  return (
    <>
      <div className="relative flex items-center justify-between">
        <Chip tone="constraint" className="h-7 px-2.5 text-[13px] font-semibold">
          Constraint
        </Chip>
        <span className="text-[13px] text-white/85">{plural(station.agents.length, 'agent')}</span>
      </div>
      <p className="relative m-0 flex items-end gap-3 md:mt-7">
        <span className={cn(numeral, 'text-[44px] md:text-[58px]')}>
          {station.in_progress}
          {station.wip_limit !== null ? <span className="text-white/70"> of {station.wip_limit}</span> : null}
        </span>
        <span className="pb-1.5 text-sm leading-snug text-white/85">
          busy,
          <br />
          {station.waiting} waiting
        </span>
      </p>
      <h2 id="constraint-h" className="relative m-0 max-w-[18ch] text-xl leading-tight font-medium md:mt-5 md:text-2xl">
        {humanize(station.id)} is holding the line back
      </h2>
      <p className="relative m-0 max-w-[34ch] text-sm leading-normal text-white/85 md:mt-3 md:text-[15px]">{history}</p>
      <AddLink station={station.id} label={`See ${humanize(station.id)} on the board`} board />
    </>
  );
}

/** No station holds the line back: the spec's copy, and the busiest station when there is one. */
function NoConstraint({ detail }: { detail?: string }) {
  const params = useFactoryParams();
  return (
    <>
      <h2 id="constraint-h" className="relative m-0 text-xl leading-tight font-medium md:mt-2 md:text-2xl">
        No constraint right now
      </h2>
      <p className="relative m-0 max-w-[34ch] text-sm leading-normal text-white/85 md:mt-3 md:text-[15px]">
        No station holds the line back. Work moves at the pace it arrives.
        {detail ? ` ${detail}` : ''}
      </p>
      <Link
        to="/o/$org/f/$factory/board"
        params={params}
        className="relative mt-2 inline-flex h-11 items-center gap-2 self-start rounded-control bg-white px-4 text-[15px] font-medium text-ink no-underline transition-colors hover:bg-shell md:mt-auto"
      >
        Open the board
        <ChevronRight size={14} strokeWidth={1.75} aria-hidden="true" />
      </Link>
    </>
  );
}

/** The card's white action: add a slot in the definition's line, or see the station on the board. */
function AddLink({ station, label, board }: { station: string; label: string; board?: boolean }) {
  const params = useFactoryParams();
  const className =
    'relative mt-2 inline-flex h-11 items-center gap-2 self-start rounded-control bg-white px-4 text-[15px] font-medium text-ink no-underline transition-colors hover:bg-shell md:mt-auto';
  const chevron = <ChevronRight size={14} strokeWidth={1.75} aria-hidden="true" />;
  return board ? (
    <Link to="/o/$org/f/$factory/board" params={params} search={{ station }} className={className}>
      {label}
      {chevron}
    </Link>
  ) : (
    <Link
      to="/o/$org/f/$factory/definition/$section"
      params={{ ...params, section: 'line' }}
      search={{ station }}
      className={className}
    >
      {label}
      {chevron}
    </Link>
  );
}
