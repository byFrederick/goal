import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { useSearch } from '@tanstack/react-router';
import { boardQuery, constraintQuery, metricsQuery, overviewQuery } from '../../api/queries';
import { useFactory } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { ActivityCard } from './activity-card';
import { FloorHeader, floorRoute } from './floor-header';
import { KpiRow } from './kpi-row';
import { LinePanel } from './line-panel';
import { NeedsYouCard } from './needs-you-card';
import { rangeOf } from './range';
import { SetupCard } from './setup-card';
import { WaitsCard } from './waits-card';
import { WaysInCard } from './ways-in-card';
import { WeeklyCard } from './weekly-card';

// The cards below the KPI tiles, as the screens draw them (ADR 0016): two columns from 768 px, Needs you
// and Where work waits across the top, Throughput by week and Activity below. A phone stacks them in
// the same order, Needs you first.
const grid = 'grid gap-4 md:grid-cols-2 md:gap-5';

// The header, the line and the flow figures come first; the cards follow. Each card loads its own data
// and shows its own loading, empty and error states.
export function FloorPage() {
  const factory = useFactory();
  const org = useOrg();
  const search = useSearch({ from: floorRoute });
  const range = rangeOf(search);
  const { data: board } = useSuspenseQuery(boardQuery(org.id, factory.id));
  const { data: metrics } = useSuspenseQuery(metricsQuery(org.id, factory.id, range));
  const { data: constraint } = useSuspenseQuery(constraintQuery(org.id, factory.id, range));
  // The week's finished items for the line's Done station; the Spend tile reads the same query.
  const week = useQuery(overviewQuery(org.id, '7d'));
  const weekRow = week.data?.factories.find((f) => f.factory.id === factory.id);
  // The lead time the what-if measures against: the range's p85, once an item finished in it.
  const leadTimeP85 = metrics.items_done === 0 ? null : (metrics.lead_time_hours?.p85 ?? null);
  const doneThisWeek = weekRow ? weekRow.throughput.days.reduce((sum, day) => sum + day.done, 0) : null;

  return (
    <div className="flex flex-col gap-4 md:gap-5">
      <FloorHeader factory={factory} board={board} metrics={metrics} />

      <div className="grid gap-4 md:gap-5 xl:grid-cols-3">
        <div className="flex min-w-0 flex-col gap-4 md:gap-5 xl:col-span-2">
          <LinePanel board={board} constraint={constraint.constraint ?? board.constraint} doneThisWeek={doneThisWeek} />
          <KpiRow factory={factory} metrics={metrics} range={range} />

          <div className={grid}>
            <NeedsYouCard />
            <WaitsCard range={range} leadTimeP85={leadTimeP85} />
            <WeeklyCard range={range} />
            <ActivityCard />
          </div>
        </div>

        {/* The setup checklist and how work gets in: beside the figures from xl, below them under it (ready.jpg). */}
        <aside className="flex min-w-0 flex-col gap-4 md:gap-5">
          <SetupCard />
          <WaysInCard />
        </aside>
      </div>
    </div>
  );
}
