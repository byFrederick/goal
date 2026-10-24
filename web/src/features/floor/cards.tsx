import { Link } from '@tanstack/react-router';
import { ChevronRight } from 'lucide-react';
import type { Board, BoardStation } from '../../api/types';
import { useFactoryParams } from '../../shared/factory';
import { humanize, plural } from '../../shared/format';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';

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
