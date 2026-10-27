import { keepPreviousData, useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { useSearch } from '@tanstack/react-router';
import { boardQuery, constraintQuery, leadTimeQuery, metricsQuery, overviewQuery, touchesQuery, weeklyQuery } from '../../api/queries';
import { useFactory } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { cn } from '../../ui/cn';
import { ConstraintCard } from './constraint-card';
import { FloorHeader, floorRoute } from './floor-header';
import { KpiRow } from './kpi-row';
import { LeadTimeCard } from './lead-time-card';
import { LinePanel } from './line-panel';
import { rangeOf, previousRange } from './range';
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

// The header, the line and the flow figures come first; the touches panel and the cards follow.
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
  // The week's finished items for the line's Done station; the Spend tile reads the same query.
  const week = useQuery(overviewQuery(org.id, '7d'));
  const weekRow = week.data?.factories.find((f) => f.factory.id === factory.id);
  const doneThisWeek = weekRow ? weekRow.throughput.days.reduce((sum, day) => sum + day.done, 0) : null;
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

  return (
    <div className="flex flex-col gap-4 md:gap-5">
      <FloorHeader factory={factory} board={board} metrics={metrics} />
      <LinePanel board={board} constraint={constraint.constraint ?? board.constraint} doneThisWeek={doneThisWeek} />
      <KpiRow factory={factory} metrics={metrics} />

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
    </div>
  );
}
