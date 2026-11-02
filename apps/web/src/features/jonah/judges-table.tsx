// The judges table: each judge's name and what it checks, the agents it scores, its sample and pass mark,
// its last 30 days as a sparkline and a rate, what its runs cost, and a switch that turns it on or off. The
// switch publishes a new version (see JudgesPage). A wide table scrolls sideways inside its card.
import { Link } from '@tanstack/react-router';
import type { JudgeListEntry } from '../../api/types';
import { formatPercent, formatUsd } from '../../shared/format';
import { useArrivals } from '../../ui/arrivals';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { NumberRoll } from '../../ui/number-roll';
import { Switch } from '../../ui/switch';
import { none, passMarkText, sampleText } from './judges-format';
import { periodRates } from './pass-rate-chart';

const bars = 7;
const barsWidth = 96;
const barsHeight = 26;
const barWidth = 8.8;
const barStep = 13.7;
const barsLeft = 2.5;
const stub = 2;

/** The last 30 days as seven square bars, the latest in ink and the earlier ones in rule2; an empty period is a flat stub. */
function Bars({ label, rates }: { label: string; rates: readonly (number | null)[] }) {
  return (
    <svg
      viewBox={`0 0 ${barsWidth} ${barsHeight}`}
      preserveAspectRatio="none"
      role="img"
      aria-label={label}
      className="block h-6 w-24 shrink-0"
    >
      {rates.map((rate, i) => {
        const h = rate === null ? stub : Math.max(Math.min(Math.max(rate, 0), 1) * barsHeight, stub);
        return (
          <rect
            // biome-ignore lint/suspicious/noArrayIndexKey: a period is its place in the window
            key={i}
            x={barsLeft + i * barStep}
            y={barsHeight - h}
            width={barWidth}
            height={h}
            className={i === bars - 1 ? 'fill-ink' : 'fill-rule'}
          />
        );
      })}
    </svg>
  );
}

export function JudgesTable({
  judges,
  org,
  factory,
  canPublish,
  switching,
  onToggle,
  today = new Date(),
}: {
  judges: readonly JudgeListEntry[];
  org: string;
  factory: string;
  /** definition:publish: a switch publishes a version, so only those who may publish flip one. */
  canPublish: boolean;
  /** The slug of the judge whose switch is publishing now; none while nothing is. */
  switching: string | undefined;
  onToggle: (judge: JudgeListEntry) => void;
  /** The window's last day; the tests fix it. */
  today?: Date;
}) {
  const arrived = useArrivals(judges.map((j) => j.slug));
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-3xl border-collapse text-secondary">
        <thead>
          <tr className="text-left text-meta text-muted">
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-medium">
              Judge
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-medium">
              Scores
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-medium">
              Sample
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-medium">
              Pass at
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-medium">
              Last 30 days, latest in black
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-medium">
              Cost
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2">
              <span className="sr-only">On</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {judges.map((judge) => {
            const { summary } = judge;
            const busy = switching === judge.slug;
            return (
              <tr key={judge.slug} className={cn('group', arrived(judge.slug) && 'motion-safe:animate-item-in')}>
                <td className="border-b border-line px-4 py-3 transition-colors duration-(--duration-quick) group-hover:bg-panel group-last:border-b-0 align-middle">
                  <div className="flex min-w-0 flex-col gap-1">
                    <Link
                      to="/o/$org/f/$factory/jonah/judges/$judge"
                      params={{ org, factory, judge: judge.slug }}
                      className="font-control font-medium text-ink hover:text-run-ink"
                    >
                      {judge.title}
                    </Link>
                    <span className="text-meta text-muted">{judge.criterion}</span>
                  </div>
                </td>
                <td className="border-b border-line px-4 py-3 transition-colors duration-(--duration-quick) group-hover:bg-panel group-last:border-b-0 align-middle">
                  {judge.agents.length === 0 ? (
                    <Chip>All agents</Chip>
                  ) : (
                    <span className="inline-flex flex-wrap gap-1">
                      {judge.agents.map((agent) => (
                        <Chip key={agent}>{agent}</Chip>
                      ))}
                    </span>
                  )}
                </td>
                <td className="border-b border-line px-4 py-3 transition-colors duration-(--duration-quick) group-hover:bg-panel group-last:border-b-0 align-middle tabular-nums text-ink-soft">
                  {sampleText(judge.sample)}
                </td>
                <td className="border-b border-line px-4 py-3 transition-colors duration-(--duration-quick) group-hover:bg-panel group-last:border-b-0 align-middle tabular-nums text-ink-soft">
                  {passMarkText(judge.pass_mark)}
                </td>
                <td className="border-b border-line px-4 py-3 transition-colors duration-(--duration-quick) group-hover:bg-panel group-last:border-b-0 align-middle">
                  <div className="flex items-center gap-3">
                    <Bars
                      label={`${judge.title}: pass rate by period, latest last`}
                      rates={periodRates(summary.days, today, bars)}
                    />
                    <span
                      className={cn(
                        'font-control font-medium tabular-nums',
                        summary.scored > 0 && summary.pass_rate < judge.pass_mark && 'text-rework-ink',
                      )}
                    >
                      {summary.scored === 0 ? none : <NumberRoll value={formatPercent(summary.pass_rate)} />}
                    </span>
                  </div>
                </td>
                <td className="border-b border-line px-4 py-3 transition-colors duration-(--duration-quick) group-hover:bg-panel group-last:border-b-0 align-middle tabular-nums text-ink-soft">
                  {formatUsd(summary.cost_usd)}
                </td>
                <td className="border-b border-line px-4 py-3 transition-colors duration-(--duration-quick) group-hover:bg-panel group-last:border-b-0 align-middle">
                  <Switch
                    checked={judge.enabled}
                    disabled={!canPublish || busy}
                    aria-label={`${judge.title} on`}
                    onCheckedChange={() => onToggle(judge)}
                  />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
