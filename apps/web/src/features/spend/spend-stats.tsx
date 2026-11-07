// The four figures over the top of the screen, as the shared stat strip: the window's spend (the budgets
// compare only with the rolling 7 days, so they are in the Budgets card, not here), this month against last,
// the cost per merged item over eight weeks, and what the rework cost. A figure goald did not give shows
// "—", never 0.
import type { OrgSpend } from '../../api/types';
import { type Stat, StatStrip } from '../../ui/stat-strip';
import { money, monthName, monthToDate, preciseMoney, spendLabel, spendTileLabel } from './view';

/** The figure as text, or null (the strip's dash) when goald did not give it. */
const given = (n: number | null, format: (n: number) => string): string | null => (n === null ? null : format(n));

export function SpendStats({ spend }: { spend: OrgSpend }) {
  const reworkPct = spend.rework_share === null ? null : Math.round(spend.rework_share * 100);

  const stats: Stat[] = [
    {
      label: spendTileLabel[spend.window],
      value: money(spend.total_usd),
    },
    {
      label: 'This month',
      value: given(spend.this_month_usd, money),
      target:
        spend.last_month_usd === null
          ? monthToDate(spend.until)
          : `${monthToDate(spend.until)} · ${monthName(lastMonthDay(spend.until))} ${money(spend.last_month_usd)}`,
    },
    {
      label: 'Cost per merged item',
      value: given(spend.cost_per_merged_usd, preciseMoney),
      target: spend.weekly.length > 0 ? '8 weeks' : undefined,
    },
    {
      label: 'Spent on rework',
      value: given(spend.rework_usd, money),
      target: reworkPct === null ? undefined : `${reworkPct}% of spend · Runs sent back`,
      note: spend.rework_usd === null ? undefined : 'Runs sent back',
    },
  ];
  return <StatStrip stats={stats} label={spendLabel[spend.window]} />;
}

/** A day in the month before the window's end, for its name: the 15th of last month, so timezones cannot move it. */
function lastMonthDay(until: string): string {
  const end = new Date(until);
  const d = new Date(Date.UTC(end.getUTCFullYear(), end.getUTCMonth() - 1, 15));
  return d.toISOString().slice(0, 10);
}
