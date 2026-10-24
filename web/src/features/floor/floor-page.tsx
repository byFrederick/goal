import { useSuspenseQuery } from '@tanstack/react-query';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { ChevronDown, ListFilter } from 'lucide-react';
import { boardQuery, metricsQuery, weeklyQuery } from '../../api/queries';
import type { Metrics } from '../../api/types';
import { factoryTitle, useFactory } from '../../shared/factory';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { DateRange } from '../../ui/date-range';
import { Menu, MenuChoice, MenuContent, MenuRadioGroup, MenuTrigger } from '../../ui/menu';
import { ConstraintCard, FlowCard } from './cards';
import { NeedsYouCard } from './needs-you';
import { lastDaysSearch, presetOf, presets, previousName, rangeLabel, todayUtc } from './range';
import { ThroughputCard } from './throughput-card';
import { WeeklyCard } from './weekly-card';

// The approved design's grid (Main and Mobile-Floor boards), laid out by the room beside the
// sidebar (container queries on main): one column with the constraint after throughput, two
// from 720 px, the three columns of the approved design from 960 px.
const grid = cn(
  'grid gap-3 md:gap-4',
  "[grid-template-areas:'tp'_'hero'_'needs'_'flow'_'wk']",
  "@[720px]:grid-cols-2 @[720px]:[grid-template-areas:'tp_hero'_'flow_needs'_'wk_wk']",
  "@[960px]:grid-cols-3 @[960px]:[grid-template-areas:'tp_flow_hero'_'wk_flow_needs']",
);

const maxRangeDays = 90;
const floorRoute = '/o/$org/f/$factory/';

export function FloorPage() {
  const factory = useFactory();
  const org = useOrg();
  const search = useSearch({ from: floorRoute });
  const { data: board } = useSuspenseQuery(boardQuery(org.id, factory.id));
  const { data: metrics } = useSuspenseQuery(metricsQuery(org.id, factory.id, search));
  const { data: weekly } = useSuspenseQuery(weeklyQuery(org.id, factory.id, search));
  const title = `${factoryTitle(factory)} floor`;
  const range = rangeLabel(metrics.from, metrics.to);
  const before = metrics.previous ? rangeLabel(metrics.previous.from, metrics.previous.to) : null;

  return (
    <>
      <header className="flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
        <div>
          <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-5xl">{title}</h1>
          <p className="m-0 mt-1 text-sm text-muted md:hidden">{phoneCaption(metrics)}</p>
        </div>
        <div className="hidden flex-wrap items-center gap-2 md:flex">
          <RangeControl metrics={metrics} before={before} label={range} />
          <WorkTypeFilter types={factory.work_types} value={search.type} />
          <NewItemButton factory={factory} />
        </div>
      </header>

      <div className={grid}>
        <ThroughputCard metrics={metrics} className="[grid-area:tp]" />
        <ConstraintCard board={board} className="[grid-area:hero]" />
        <NeedsYouCard board={board} factory={factory} className="[grid-area:needs]" />
        <FlowCard board={board} className="[grid-area:flow]" />
        <WeeklyCard weekly={weekly} className="[grid-area:wk]" />
      </div>
    </>
  );
}

/** "Oct 17 to 23, vs the week before", as the phone's header says it. */
function phoneCaption(m: Metrics): string {
  const before = previousName(m.from, m.to);
  const vs = before === 'last week' ? 'the week before' : before;
  return `${rangeLabel(m.from, m.to)}, vs ${vs}${m.type ? `, ${m.type} only` : ''}`;
}

/** The range toggle and the range it is compared with, writing ?from&to (the type is kept). */
function RangeControl({ metrics, before, label }: { metrics: Metrics; before: string | null; label: string }) {
  const navigate = useNavigate({ from: floorRoute });
  const type = metrics.type ?? undefined;
  return (
    <div className="inline-flex h-10 items-center gap-1 rounded-[10px] bg-control p-[3px]">
      <DateRange
        label={label}
        from={metrics.from}
        to={metrics.to}
        today={todayUtc()}
        presets={presets}
        active={presetOf(metrics.from, metrics.to)}
        maxDays={maxRangeDays}
        onPreset={(days) => navigate({ search: lastDaysSearch(days, type), resetScroll: false })}
        onRange={(from, to) => navigate({ search: { from, to, ...(type ? { type } : {}) }, resetScroll: false })}
      />
      {before ? <span className="px-2.5 text-sm text-muted tabular-nums">vs {before}</span> : null}
    </div>
  );
}

const allTypes = 'all';

/** "All work types" or one of the factory's, writing ?type (the range is kept). */
function WorkTypeFilter({ types, value }: { types: readonly string[]; value: string | undefined }) {
  const navigate = useNavigate({ from: floorRoute });
  if (types.length === 0) return null;
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="secondary" aria-label={`Work type: ${value ?? 'all work types'}`}>
          <ListFilter size={16} strokeWidth={1.5} aria-hidden="true" />
          {value ?? 'All work types'}
          <ChevronDown size={14} strokeWidth={1.75} aria-hidden="true" />
        </Button>
      </MenuTrigger>
      <MenuContent side="bottom" align="end" className="w-60">
        <MenuRadioGroup
          value={value ?? allTypes}
          onValueChange={(next) =>
            navigate({
              search: ({ type: _, ...range }) => (next === allTypes ? range : { ...range, type: next }),
              resetScroll: false,
            })
          }
        >
          <MenuChoice value={allTypes}>All work types</MenuChoice>
          {types.map((t) => (
            <MenuChoice key={t} value={t}>
              {t}
            </MenuChoice>
          ))}
        </MenuRadioGroup>
      </MenuContent>
    </Menu>
  );
}
