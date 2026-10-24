import { useSuspenseQuery } from '@tanstack/react-query';
import { CalendarDays } from 'lucide-react';
import { boardQuery, metricsQuery } from '../../api/queries';
import { factoryTitle, useFactory } from '../../shared/factory';
import { formatDay } from '../../shared/format';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { cn } from '../../ui/cn';
import { ConstraintCard, DaysCard, FlowCard, ThroughputCard } from './cards';
import { NeedsYouCard } from './needs-you';

// Narrow screens stack the cards with the constraint near the top; two columns from 720 px of
// room beside the sidebar, the three columns of the approved design from 960 px (container
// queries on main, so the sidebar's width counts).
const grid = cn(
  'grid gap-3 md:gap-4',
  "[grid-template-areas:'tp'_'hero'_'needs'_'flow'_'days']",
  "@[720px]:grid-cols-2 @[720px]:[grid-template-areas:'tp_hero'_'flow_needs'_'days_days']",
  "@[960px]:grid-cols-3 @[960px]:[grid-template-areas:'tp_flow_hero'_'days_flow_needs']",
);

export function FloorPage() {
  const factory = useFactory();
  const org = useOrg();
  const { data: board } = useSuspenseQuery(boardQuery(org.id, factory.id));
  const { data: metrics } = useSuspenseQuery(metricsQuery(org.id, factory.id));
  const title = `${factoryTitle(factory)} floor`;
  const days = metrics.throughput;
  const range = days.length > 0 ? `${formatDay(days[0]?.day ?? '')} to ${formatDay(days.at(-1)?.day ?? '')}` : '';

  return (
    <>
      <header className="flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
        <div>
          <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-5xl">{title}</h1>
          <p className="m-0 mt-1 text-sm text-muted md:hidden">
            Last {days.length} days, {range}
          </p>
        </div>
        <div className="hidden flex-wrap items-center gap-2 md:flex">
          <span className="inline-flex h-10 items-center gap-2 rounded-[10px] bg-control p-[3px] pr-2.5 text-sm text-muted">
            <span className="inline-flex h-full items-center gap-2 rounded-control bg-surface px-3 text-ink shadow-raise">
              <CalendarDays size={16} strokeWidth={1.5} aria-hidden="true" />
              {range}
            </span>
            Last {days.length} days
          </span>
          <NewItemButton factory={factory} />
        </div>
      </header>

      <div className={grid}>
        <ThroughputCard metrics={metrics} className="[grid-area:tp]" />
        <ConstraintCard board={board} className="[grid-area:hero]" />
        <NeedsYouCard board={board} factory={factory} className="[grid-area:needs]" />
        <FlowCard board={board} className="[grid-area:flow]" />
        <DaysCard metrics={metrics} className="[grid-area:days]" />
      </div>
    </>
  );
}
