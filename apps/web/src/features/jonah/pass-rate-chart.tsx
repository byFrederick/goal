// One judge's pass rate over the last 30 days (jonah.html, "Pass rate by judge"): nine square bars, one per
// period of about three days. The latest period is ink, the earlier ones rule, and the judge's pass mark is a
// dashed muted line. A period with nothing scored is a flat stub, never a missing bar. The card links to the
// judge's page, and a table that only a screen reader sees gives the values.
import { Link } from '@tanstack/react-router';
import type { JudgeListEntry } from '../../api/types';
import { useFactoryParams } from '../../shared/factory';
import { formatPercent } from '../../shared/format';
import { cn } from '../../ui/cn';
import { NumberRoll } from '../../ui/number-roll';
import { passMarkText } from './judges-format';

const periods = 9;
const windowDays = 30;
const width = 200;
const height = 46;
const barWidth = 14.2;
const step = 22.2;
const left = 4;
const stub = 2;
const msPerDay = 86_400_000;

/**
 * The pass rate of each of `count` periods (nine by default), oldest first, or null where nothing was scored.
 * The window ends on `today`'s UTC day. The judges table calls it with seven.
 */
export function periodRates(
  days: JudgeListEntry['summary']['days'],
  today: Date,
  count: number = periods,
): (number | null)[] {
  const end = Math.floor(today.getTime() / msPerDay);
  const scored = new Array<number>(count).fill(0);
  const passed = new Array<number>(count).fill(0);
  for (const day of days) {
    const age = end - Math.floor(Date.parse(`${day.day}T00:00:00Z`) / msPerDay);
    if (age < 0 || age >= windowDays) continue;
    const period = Math.floor(((windowDays - 1 - age) * count) / windowDays);
    scored[period] = (scored[period] ?? 0) + day.scored;
    passed[period] = (passed[period] ?? 0) + day.passed;
  }
  return scored.map((n, i) => (n === 0 ? null : (passed[i] ?? 0) / n));
}

/** The change from the first period with scores to the last one, in whole points; null with fewer than two. */
export function changeInPoints(rates: readonly (number | null)[]): number | null {
  const known = rates.filter((rate): rate is number => rate !== null);
  const first = known[0];
  const last = known[known.length - 1];
  if (known.length < 2 || first === undefined || last === undefined) return null;
  return Math.round((last - first) * 100);
}

function summaryOf(rates: readonly (number | null)[], passMark: number): string {
  const known = rates.filter((rate): rate is number => rate !== null);
  const first = known[0];
  const last = known[known.length - 1];
  const mark = `pass mark ${formatPercent(passMark)}`;
  if (first === undefined || last === undefined) return `Nothing scored, ${mark}`;
  if (known.length < 2) return `${formatPercent(last)}, ${mark}`;
  return `From ${formatPercent(first)} to ${formatPercent(last)}, ${mark}`;
}

function Change({ points }: { points: number | null }) {
  if (points === null) return null;
  if (points === 0) return <span className="text-meta text-muted">steady</span>;
  return (
    <span className={cn('text-meta font-control', points > 0 ? 'text-ok-ink' : 'text-rework-ink')}>
      {points > 0 ? `+${points} pts` : `−${Math.abs(points)} pts`}
    </span>
  );
}

export function PassRateChart({ judge, today = new Date() }: { judge: JudgeListEntry; today?: Date }) {
  const params = useFactoryParams();
  const rates = periodRates(judge.summary.days, today);
  const bars = rates.map((rate, i) => ({ rate, n: i + 1 }));
  const scale = (rate: number) => height - Math.min(Math.max(rate, 0), 1) * (height - stub);
  const markY = scale(judge.pass_mark);
  const agents = judge.agents.length === 0 ? 'All agents' : judge.agents.join(', ');
  const scored = judge.summary.scored > 0;
  return (
    <article className="relative flex min-w-0 flex-col gap-2 rounded-card border border-line bg-card p-4 transition-colors duration-(--duration-quick) hover:border-rule">
      <Link
        to="/o/$org/f/$factory/jonah/judges/$judge"
        params={{ ...params, judge: judge.slug }}
        className="press text-secondary font-control text-ink-soft no-underline after:absolute after:inset-0 after:rounded-card focus-visible:outline-none focus-visible:after:outline-2 focus-visible:after:outline-offset-2 focus-visible:after:outline-run"
      >
        {judge.title}
      </Link>
      <span className="flex items-baseline gap-2">
        <span className="text-kpi font-control tabular-nums">
          {scored ? <NumberRoll value={formatPercent(judge.summary.pass_rate)} /> : '—'}
        </span>
        {scored ? (
          <Change points={changeInPoints(rates)} />
        ) : (
          <span className="text-meta text-muted">No scores yet</span>
        )}
      </span>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        role="img"
        aria-label={summaryOf(rates, judge.pass_mark)}
        className="block h-11.5 w-full"
      >
        {bars.map(({ rate, n }) => {
          const i = n - 1;
          const y = rate === null ? height - stub : scale(rate);
          const key = `period-${n}`;
          return (
            <rect
              key={key}
              x={left + i * step}
              y={y}
              width={barWidth}
              height={height - y}
              className={i === periods - 1 ? 'fill-ink' : 'fill-rule'}
            />
          );
        })}
        <line
          x1={0}
          x2={width}
          y1={markY}
          y2={markY}
          className="stroke-muted"
          strokeDasharray="4 3"
          vectorEffect="non-scaling-stroke"
        />
      </svg>
      <span className="text-meta text-muted">{`${agents}, pass at ${passMarkText(judge.pass_mark)}`}</span>
      <table className="sr-only">
        <caption>{`${judge.title}, pass rate by period of about three days, oldest first`}</caption>
        <thead>
          <tr>
            <th scope="col">Period</th>
            <th scope="col">Pass rate</th>
          </tr>
        </thead>
        <tbody>
          {bars.map(({ rate, n }) => (
            <tr key={`period-${n}`}>
              <th scope="row">{n}</th>
              <td>{rate === null ? 'Nothing scored' : formatPercent(rate)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </article>
  );
}
