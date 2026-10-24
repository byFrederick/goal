import { Link } from '@tanstack/react-router';
import { ChevronRight } from 'lucide-react';
import type { Board, BoardStation } from '../../api/types';
import { useFactoryParams } from '../../shared/factory';
import { humanize, plural } from '../../shared/format';
import { waitingOnYou } from '../../shared/item-state';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Hatch, type HatchTone } from '../../ui/hatch';

// Numbers are tabular (MASTER.md); names around them stay proportional.
const numeral = 'm-0 font-normal tracking-numeral leading-none tabular-nums';

/** The dark card: the station holding the line back (the drum), or that there is none. */
export function ConstraintCard({ board, className }: { board: Board; className?: string }) {
  const station = board.stations.find((s) => s.id === board.constraint);
  const shapes = (
    <>
      <span
        aria-hidden="true"
        className="pointer-events-none absolute -right-12 -bottom-16 size-56 rounded-full border-[1.5px] border-white/20"
      />
      <span
        aria-hidden="true"
        className="pointer-events-none absolute right-6 bottom-8 hidden h-28 w-11 rounded-l-control border-[1.5px] border-r-0 border-white/35 md:block"
      />
    </>
  );

  return (
    <section
      aria-labelledby="constraint-h"
      className={cn(
        'relative flex min-h-0 flex-col gap-2.5 overflow-hidden rounded-card bg-ink p-4 text-white md:min-h-[420px] md:gap-0 md:p-[22px]',
        className,
      )}
    >
      {shapes}
      {station ? (
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
          <h2
            id="constraint-h"
            className="relative m-0 max-w-[18ch] text-xl leading-tight font-medium md:mt-5 md:text-2xl"
          >
            {humanize(station.id)} is holding the line back
          </h2>
          <p className="relative m-0 max-w-[34ch] text-sm leading-normal text-white/85 md:mt-3 md:text-[15px]">
            {station.waiting > 0
              ? `${plural(station.waiting, 'item')} wait in front of it. The rest of the line moves at its pace.`
              : 'It runs at its limit. The rest of the line moves at its pace.'}
          </p>
          <ConstraintLink station={station} />
        </>
      ) : (
        <>
          <h2 id="constraint-h" className="relative m-0 text-xl leading-tight font-medium md:mt-2 md:text-2xl">
            No constraint right now
          </h2>
          <p className="relative m-0 max-w-[34ch] text-sm leading-normal text-white/85 md:mt-3 md:text-[15px]">
            No station holds the line back. Work moves at the pace it arrives.
          </p>
          <ConstraintLink />
        </>
      )}
    </section>
  );
}

function ConstraintLink({ station }: { station?: BoardStation }) {
  const params = useFactoryParams();
  return (
    <Link
      to="/o/$org/f/$factory/board"
      params={params}
      search={station ? { station: station.id } : {}}
      className="relative mt-2 inline-flex h-11 items-center gap-2 self-start rounded-control bg-white px-4 text-[15px] font-medium text-ink no-underline transition-colors hover:bg-shell md:mt-auto"
    >
      {station ? `See ${humanize(station.id)} on the board` : 'Open the board'}
      <ChevronRight size={14} strokeWidth={1.75} aria-hidden="true" />
    </Link>
  );
}

interface Load {
  station: BoardStation;
  inProgress: number;
  waiting: number;
  blocked: number;
}

const legend: { tone: HatchTone; label: string }[] = [
  { tone: 'run', label: 'In progress' },
  { tone: 'target', label: 'Waiting for an agent' },
  { tone: 'you', label: 'Waiting on you' },
  { tone: 'rework', label: 'Blocked' },
];

/** Where items sit along the line, station by station; the constraint is marked. */
export function FlowCard({ board, className }: { board: Board; className?: string }) {
  const loads: Load[] = board.stations
    .filter((s) => s.kind !== 'done')
    .map((s) => ({
      station: s,
      inProgress: s.in_progress,
      waiting: s.kind === 'human' ? waitingOnYou(s) : s.waiting,
      blocked: s.items.filter((it) => it.state === 'blocked').length,
    }));
  const max = Math.max(1, ...loads.map((l) => l.inProgress + l.waiting + l.blocked));
  const totalWaiting = loads.reduce((n, l) => n + l.waiting, 0);
  const onYou = loads.filter((l) => l.station.kind === 'human').reduce((n, l) => n + l.waiting, 0);
  const atConstraint = loads.find((l) => l.station.id === board.constraint);

  return (
    <SectionCard
      title="Where work waits"
      description={`${plural(totalWaiting, 'item')} waiting, ${loads.reduce((n, l) => n + l.inProgress, 0)} in progress`}
      className={className}
    >
      <ol className="m-0 mt-1 flex list-none flex-col gap-4 p-0">
        {loads.map((l) => {
          const segments: { tone: HatchTone; n: number }[] = [
            { tone: 'run', n: l.inProgress },
            { tone: l.station.kind === 'human' ? 'you' : 'target', n: l.waiting },
            { tone: 'rework', n: l.blocked },
          ];
          const parts = [
            l.waiting > 0 ? `${l.waiting} waiting${l.station.kind === 'human' ? ' on you' : ''}` : '',
            l.station.kind === 'agent'
              ? `${l.inProgress}${l.station.wip_limit !== null ? ` of ${l.station.wip_limit}` : ''} busy`
              : '',
            l.blocked > 0 ? `${l.blocked} blocked` : '',
          ].filter(Boolean);
          return (
            <li key={l.station.id} className="flex flex-col gap-2">
              <div className="flex items-center justify-between gap-3 text-[15px]">
                <span className="flex items-center gap-2">
                  {humanize(l.station.id)}
                  {l.station.id === board.constraint ? <Chip tone="constraint">Constraint</Chip> : null}
                </span>
                <span className={cn('text-sm tabular-nums', parts.length ? 'text-ink' : 'text-muted')}>
                  {parts.length ? parts.join(', ') : 'Empty'}
                </span>
              </div>
              <div aria-hidden="true" className="flex h-5 gap-0.5">
                {segments.map((seg) =>
                  seg.n > 0 ? (
                    <div
                      key={seg.tone}
                      className="h-full overflow-hidden rounded-[3px]"
                      style={{ width: `${(seg.n / max) * 100}%` }}
                    >
                      <Hatch tone={seg.tone} />
                    </div>
                  ) : null,
                )}
                {l.inProgress + l.waiting + l.blocked === 0 ? (
                  <div className="h-full w-full rounded-[3px] bg-surface" />
                ) : null}
              </div>
            </li>
          );
        })}
      </ol>
      <ul aria-label="Legend" className="m-0 flex list-none flex-wrap gap-x-4 gap-y-1.5 p-0 text-[13px] text-muted">
        {legend.map((l) => (
          <li key={l.tone} className="flex items-center gap-1.5">
            <span className="block size-3 overflow-hidden rounded-[2px]">
              <Hatch tone={l.tone} />
            </span>
            {l.label}
          </li>
        ))}
      </ul>
      {atConstraint && totalWaiting > 0 ? (
        <div className="mt-auto rounded-control border border-line bg-surface px-4 py-3.5">
          <p className="m-0 text-[15px] leading-snug">
            {atConstraint.waiting} of {plural(totalWaiting, 'waiting item')} sit in front of{' '}
            {humanize(atConstraint.station.id)}.
          </p>
          <p className="m-0 mt-1.5 text-sm text-muted">
            It is the constraint{onYou > 0 ? `; ${plural(onYou, 'item')} wait on you.` : '.'}
          </p>
        </div>
      ) : null}
    </SectionCard>
  );
}
