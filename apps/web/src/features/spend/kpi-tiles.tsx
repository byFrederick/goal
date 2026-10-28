// The four figures over the top of the screen: the window's spend against the weekly budgets (the bar
// only for the week, whose budgets are weekly), this month against last, the cost per merged item over
// eight weeks, and what the rework cost. A figure goald did not give shows "—", never 0.
import type { OrgSpend } from '../../api/types';
import { KpiTile } from '../../ui/kpi-tile';
import { money, monthName, monthToDate, percentOf, preciseMoney, spendLabel, sumOf } from './view';

export function KpiTiles({ spend }: { spend: OrgSpend }) {
  const weeklyCaps = sumOf(spend.budgets.map((b) => b.weekly_usd));
  const weekBar =
    spend.window === 'week' && weeklyCaps !== null && weeklyCaps > 0
      ? {
          value: spend.total_usd,
          max: weeklyCaps,
          tone: 'run' as const,
          label: `${percentOf(spend.total_usd, weeklyCaps)}% of the weekly budgets combined`,
        }
      : undefined;
  const points = spend.weekly.map((w) => w.cost_per_merged_usd);
  const reworkPct = spend.rework_share === null ? null : Math.round(spend.rework_share * 100);

  return (
    <section aria-label={spendLabel[spend.window]} className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <KpiTile
        label={spendLabel[spend.window]}
        value={money(spend.total_usd)}
        unit={weekBar ? `of ${money(weeklyCaps)}` : undefined}
        bar={weekBar}
        target={weekBar ? 'All factory budgets' : undefined}
      />
      <KpiTile
        label="This month"
        value={money(spend.this_month_usd)}
        target={monthToDate(spend.until)}
        note={
          spend.last_month_usd === null
            ? undefined
            : `${monthName(lastMonthDay(spend.until))} ${money(spend.last_month_usd)}`
        }
      />
      <KpiTile
        label="Cost per merged item"
        value={preciseMoney(spend.cost_per_merged_usd)}
        sparkline={
          points.length >= 2 && points.every((p) => p !== null)
            ? {
                points: points as number[],
                label: `Cost per merged item over 8 weeks, from ${preciseMoney(points[0])} to ${preciseMoney(points.at(-1))}`,
              }
            : undefined
        }
        target={points.length > 0 ? '8 weeks' : undefined}
      />
      <KpiTile
        label="Spent on rework"
        value={money(spend.rework_usd)}
        bar={
          reworkPct === null
            ? undefined
            : { value: reworkPct, max: 100, tone: 'rework', label: `${reworkPct}% of this window's spend` }
        }
        target={reworkPct === null ? undefined : `${reworkPct}% of spend`}
        note={spend.rework_usd === null ? undefined : 'Runs sent back'}
      />
    </section>
  );
}

/** A day in the month before the window's end, for its name: the 15th of last month, so timezones cannot move it. */
function lastMonthDay(until: string): string {
  const end = new Date(until);
  const d = new Date(Date.UTC(end.getUTCFullYear(), end.getUTCMonth() - 1, 15));
  return d.toISOString().slice(0, 10);
}
