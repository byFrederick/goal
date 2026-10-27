// Throughput by week on the Floor (Floor, Throughput by week): items merged a day in each of the last 12
// weeks as bars, the latest solid, against the throughput target as a dashed line.
import { useQuery } from '@tanstack/react-query';
import { weeklyQuery } from '../../api/queries';
import type { WeeklyThroughput } from '../../api/types';
import { useFactory } from '../../shared/factory';
import { formatDay } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Bars } from '../../ui/bars';
import { SectionCard } from '../../ui/card';
import { CardEmpty, CardFailed, CardLoading } from './card-states';
import { type FloorRange, rangeLabel } from './range';

const perDay = (n: number) => n.toFixed(1);
/** A target as it was set: "8", "2.5". */
const targetOf = (n: number) => (Number.isInteger(n) ? `${n}` : n.toFixed(1));

export function WeeklyCard({ range, className }: { range: FloorRange; className?: string }) {
  const org = useOrg();
  const factory = useFactory();
  const query = useQuery(weeklyQuery(org.id, factory.id, range));

  return (
    <SectionCard
      title="Throughput by week"
      aside={<span className="text-meta text-muted">Items merged a day</span>}
      aria-busy={query.isPending || undefined}
      className={className}
    >
      {query.isPending ? <CardLoading /> : null}
      {query.isError ? <CardFailed what="throughput by week" onRetry={() => void query.refetch()} /> : null}
      {query.data && query.data.weeks.length === 0 ? (
        <CardEmpty>No finished items in these weeks yet.</CardEmpty>
      ) : null}
      {query.data && query.data.weeks.length > 0 ? <Ready weekly={query.data} /> : null}
    </SectionCard>
  );
}

function Ready({ weekly }: { weekly: WeeklyThroughput }) {
  const weeks = weekly.weeks;
  const target = weekly.targets.throughput_per_day;
  const first = weeks[0];
  const last = weeks.at(-1);
  const values = weeks.map((w) => w.per_day);
  const summary =
    first && last
      ? `Items merged a day by week, from ${formatDay(first.from)} to ${formatDay(last.from)}, between ${perDay(Math.min(...values))} and ${perDay(Math.max(...values))} a day.${target !== null ? ` Target ${targetOf(target)}.` : ''}`
      : 'No weeks to show.';

  return (
    <>
      <Bars
        bars={weeks.map((w) => ({
          key: w.from,
          value: w.per_day,
          label: rangeLabel(w.from, w.to),
          detail: `${perDay(w.per_day)} a day, ${w.done} merged`,
        }))}
        target={target}
        targetLabel={target !== null ? `Target ${targetOf(target)}` : ''}
        summary={summary}
        caption="Items merged per day, by week"
        columns={['Week', 'Merged']}
        className="h-44"
      />
      {first ? (
        <div className="flex justify-between gap-3 text-secondary text-muted tabular-nums">
          <span>{formatDay(first.from)}</span>
          <span>This week</span>
        </div>
      ) : null}
    </>
  );
}
