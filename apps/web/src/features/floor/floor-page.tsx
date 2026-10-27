import { keepPreviousData, useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { ChevronDown, ListFilter } from 'lucide-react';
import { boardQuery, constraintQuery, leadTimeQuery, metricsQuery, touchesQuery, weeklyQuery } from '../../api/queries';
import type { Metrics } from '../../api/types';
import { factoryTitle, useFactory } from '../../shared/factory';
import { InboxButton } from '../../shared/inbox-drawer';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { DateRange } from '../../ui/date-range';
import { Menu, MenuChoice, MenuContent, MenuRadioGroup, MenuTrigger } from '../../ui/menu';
import { ConstraintCard } from './constraint-card';
import { LeadTimeCard } from './lead-time-card';
import {
  type FloorSearch,
  lastDaysSearch,
  presetOf,
  presets,
  previousName,
  previousRange,
  rangeLabel,
  rangeOf,
  todayUtc,
} from './range';
import { ThroughputCard } from './throughput-card';
import { TouchesPanel, TouchesSummary, TouchesWaiting } from './touches-card';
import { WeeklyCard } from './weekly-card';

// The approved design's grid (Main and Mobile-Floor boards), laid out by the room beside the
// sidebar (container queries on main): one column with the constraint after throughput, two
// from 720 px with the lead time across the foot, the three columns of the approved design from
// 960 px (the constraint runs down the right, the lead time down the middle). The human touches
// panel runs across the top from 768 px; on a phone their card follows Throughput, in its cell.
const grid = cn(
  'grid gap-3 md:gap-4',
  "[grid-template-areas:'tp'_'hero'_'flow'_'wk']",
  "@[720px]:grid-cols-2 @[720px]:[grid-template-areas:'tp_hero'_'flow_flow'_'wk_wk']",
  "@[960px]:grid-cols-3 @[960px]:[grid-template-areas:'tp_flow_hero'_'wk_flow_hero']",
);

const maxRangeDays = 90;
const floorRoute = '/o/$org/f/$factory/';

export function FloorPage() {
  const factory = useFactory();
  const org = useOrg();
  const search = useSearch({ from: floorRoute });
  const range = rangeOf(search);
  const { data: board } = useSuspenseQuery(boardQuery(org.id, factory.id));
  const { data: metrics } = useSuspenseQuery(metricsQuery(org.id, factory.id, range));
  const { data: weekly } = useSuspenseQuery(weeklyQuery(org.id, factory.id, range));
  const { data: bridge } = useSuspenseQuery(leadTimeQuery(org.id, factory.id, range));
  const { data: constraint } = useSuspenseQuery(constraintQuery(org.id, factory.id, range));
  // The touches never hold the Floor up: a range change keeps the last ones until the new arrive.
  const touches = useQuery({ ...touchesQuery(org.id, factory.id, range), placeholderData: keepPreviousData });
  const touchesBefore = useQuery({
    ...touchesQuery(org.id, factory.id, previousRange(range)),
    placeholderData: keepPreviousData,
  });
  const retryTouches = () => {
    void touches.refetch();
    void touchesBefore.refetch();
  };
  const title = `${factoryTitle(factory)} floor`;
  const label = rangeLabel(metrics.from, metrics.to);
  const before = metrics.previous ? rangeLabel(metrics.previous.from, metrics.previous.to) : null;

  return (
    <>
      <header className="flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
        <div>
          <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-5xl">{title}</h1>
          <p className="m-0 mt-1 text-sm text-muted md:hidden">{phoneCaption(metrics)}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <InboxButton factory={factory} />
          <div className="hidden flex-wrap items-center gap-2 md:flex">
            <RangeControl metrics={metrics} before={before} label={label} />
            <WorkTypeFilter types={factory.work_types} value={search.type} />
            <NewItemButton factory={factory} />
          </div>
        </div>
      </header>

      {touches.data ? (
        <TouchesPanel touches={touches.data} previous={touchesBefore.data} grouping={search.touches ?? 'station'} />
      ) : (
        <TouchesWaiting failed={touches.isError} onRetry={retryTouches} className="hidden md:flex" />
      )}

      <div className={grid}>
        <div className="flex flex-col gap-3 [grid-area:tp]">
          <ThroughputCard metrics={metrics} className="md:flex-1" />
          {touches.data ? (
            <TouchesSummary touches={touches.data} previous={touchesBefore.data} />
          ) : (
            <TouchesWaiting failed={touches.isError} onRetry={retryTouches} className="md:hidden" />
          )}
        </div>
        <ConstraintCard report={constraint} board={board} className="[grid-area:hero]" />
        <LeadTimeCard bridge={bridge} factory={factory} className="[grid-area:flow]" />
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

/** The touches grouping of the search, kept when the range changes. */
const keepGrouping = ({ touches }: FloorSearch) => (touches ? { touches } : {});

/** The range toggle and the range it is compared with, writing ?from&to (the type is kept). */
function RangeControl({ metrics, before, label }: { metrics: Metrics; before: string | null; label: string }) {
  const navigate = useNavigate({ from: floorRoute });
  const type = metrics.type ?? undefined;
  return (
    <div className="inline-flex h-10 items-center gap-1 rounded-[10px] bg-panel p-[3px]">
      <DateRange
        label={label}
        from={metrics.from}
        to={metrics.to}
        today={todayUtc()}
        presets={presets}
        active={presetOf(metrics.from, metrics.to)}
        maxDays={maxRangeDays}
        onPreset={(days) =>
          navigate({ search: (prev) => ({ ...lastDaysSearch(days, type), ...keepGrouping(prev) }), resetScroll: false })
        }
        onRange={(from, to) =>
          navigate({
            search: (prev) => ({ from, to, ...(type ? { type } : {}), ...keepGrouping(prev) }),
            resetScroll: false,
          })
        }
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
