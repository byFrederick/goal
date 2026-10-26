import type { WeeklyThroughput } from '../../api/types';
import { formatDay, plural } from '../../shared/format';
import { Bars } from '../../ui/bars';
import { SectionCard } from '../../ui/card';
import { rangeLabel } from './range';

const perDay = (n: number) => n.toFixed(1);
/** A target as it was set: "8", "2.5". */
const targetOf = (n: number) => (Number.isInteger(n) ? `${n}` : n.toFixed(1));

/**
 * Items finished a day in each of the last 12 weeks, the latest solid, against the throughput
 * target (Main: Last 12 weeks).
 */
export function WeeklyCard({ weekly, className }: { weekly: WeeklyThroughput; className?: string }) {
  const weeks = weekly.weeks;
  const target = weekly.targets.throughput_per_day;
  const first = weeks[0];
  const last = weeks.at(-1);
  const values = weeks.map((w) => w.per_day);
  const low = Math.min(...values);
  const high = Math.max(...values);
  const summary =
    first && last
      ? `Throughput by week from ${formatDay(first.from)} to ${formatDay(last.from)}, between ${perDay(low)} and ${perDay(high)} a day. The last week ${perDay(last.per_day)}.${target !== null ? ` Target ${targetOf(target)}.` : ''}`
      : 'No weeks to show.';

  return (
    <SectionCard
      title={weeks.length === 1 ? 'Last week' : `Last ${plural(weeks.length, 'week')}`}
      aside={<span className="text-sm text-muted">Throughput by week</span>}
      className={className}
    >
      <Bars
        bars={weeks.map((w) => ({
          key: w.from,
          value: w.per_day,
          label: rangeLabel(w.from, w.to),
          detail: `${perDay(w.per_day)} a day, ${w.done} finished`,
        }))}
        target={target}
        targetLabel={target !== null ? `Target ${targetOf(target)}` : ''}
        summary={summary}
        caption="Items finished per day, by week"
        columns={['Week', 'Finished']}
        className="mt-6 h-[180px] md:mt-8"
      />
      {first && last ? (
        <div className="flex justify-between gap-3 text-[13px] text-muted tabular-nums">
          <span>{formatDay(first.from)}</span>
          <span>
            {formatDay(last.from)}: <strong className="font-medium text-ink">{perDay(last.per_day)}</strong>
          </span>
        </div>
      ) : null}
    </SectionCard>
  );
}
