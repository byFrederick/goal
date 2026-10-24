import { useSuspenseQuery } from '@tanstack/react-query';
import { CalendarDays } from 'lucide-react';
import { boardQuery, metricsQuery } from '../../api/queries';
import { FactorySwitcher, LiveStatus } from '../../shared/chrome';
import { factoryTitle, useFactory } from '../../shared/factory';
import { formatDay } from '../../shared/format';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { cn } from '../../ui/cn';
import { LogoMark } from '../../ui/logo';
import { ConstraintCard, DaysCard, FlowCard, ThroughputCard } from './cards';
import { NeedsYouCard } from './needs-you';

// Phones stack the cards with the constraint near the top; from 1024 px two columns, from
// 1280 px the three columns of the approved design.
const grid = cn(
  'grid gap-3 md:gap-4',
  "[grid-template-areas:'tp'_'hero'_'needs'_'flow'_'days']",
  "lg:grid-cols-2 lg:[grid-template-areas:'tp_hero'_'flow_needs'_'days_days']",
  "xl:grid-cols-3 xl:[grid-template-areas:'tp_flow_hero'_'days_flow_needs']",
);

export function FloorPage() {
  const factory = useFactory();
  const org = useOrg();
  const { data: board } = useSuspenseQuery(boardQuery(org, factory.name));
  const { data: metrics } = useSuspenseQuery(metricsQuery(org, factory.name));
  const title = `${factoryTitle(factory)} floor`;
  const days = metrics.throughput;
  const range = days.length > 0 ? `${formatDay(days[0]?.day ?? '')} to ${formatDay(days.at(-1)?.day ?? '')}` : '';

  return (
    <>
      <div className="flex items-center justify-between md:hidden">
        <LogoMark />
        <FactorySwitcher factory={factory} />
        <LiveStatus compact className="w-[30px] justify-end" />
      </div>

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
        <ConstraintCard board={board} factory={factory.name} className="[grid-area:hero]" />
        <NeedsYouCard board={board} factory={factory} className="[grid-area:needs]" />
        <FlowCard board={board} className="[grid-area:flow]" />
        <DaysCard metrics={metrics} className="[grid-area:days]" />
      </div>
    </>
  );
}
