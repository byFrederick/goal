// The judges table: each judge's name and what it checks, the agents it scores, its sample and pass mark,
// its last 30 days as a sparkline and a rate, what its runs cost, and a switch that turns it on or off. The
// switch publishes a new version (see JudgesPage). A wide table scrolls sideways inside its card.
import { Link } from '@tanstack/react-router';
import type { JudgeListEntry } from '../../api/types';
import { formatPercent, formatUsd } from '../../shared/format';
import { Chip } from '../../ui/chip';
import { Sparkline } from '../../ui/kpi-tile';
import { Switch } from '../../ui/switch';
import { none, passMarkText, sampleText } from './judges-format';

export function JudgesTable({
  judges,
  org,
  factory,
  canPublish,
  switching,
  onToggle,
}: {
  judges: readonly JudgeListEntry[];
  org: string;
  factory: string;
  /** definition:publish: a switch publishes a version, so only those who may publish flip one. */
  canPublish: boolean;
  /** The slug of the judge whose switch is publishing now; none while nothing is. */
  switching: string | undefined;
  onToggle: (judge: JudgeListEntry) => void;
}) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-3xl border-collapse text-secondary">
        <thead>
          <tr className="text-left text-meta text-muted">
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-semibold">
              Judge
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-semibold">
              Scores
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-semibold">
              Sample
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-semibold">
              Pass at
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-semibold">
              Last 30 days
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-semibold">
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
              <tr key={judge.slug} className="hover:bg-panel">
                <td className="border-b border-line px-4 py-3 align-middle">
                  <div className="flex min-w-0 flex-col gap-1">
                    <Link
                      to="/o/$org/f/$factory/jonah/judges/$judge"
                      params={{ org, factory, judge: judge.slug }}
                      className="font-control font-semibold text-ink hover:text-run-ink"
                    >
                      {judge.title}
                    </Link>
                    <span className="text-meta text-muted">{judge.criterion}</span>
                  </div>
                </td>
                <td className="border-b border-line px-4 py-3 align-middle">
                  {judge.agents.length === 0 ? (
                    <span className="text-ink-soft">All agents</span>
                  ) : (
                    <span className="inline-flex flex-wrap gap-1">
                      {judge.agents.map((agent) => (
                        <Chip key={agent}>{agent}</Chip>
                      ))}
                    </span>
                  )}
                </td>
                <td className="border-b border-line px-4 py-3 align-middle tabular-nums text-ink-soft">
                  {sampleText(judge.sample)}
                </td>
                <td className="border-b border-line px-4 py-3 align-middle tabular-nums text-ink-soft">
                  {passMarkText(judge.pass_mark)}
                </td>
                <td className="border-b border-line px-4 py-3 align-middle">
                  <div className="flex items-center gap-2.5">
                    {summary.days.length > 1 ? (
                      <Sparkline
                        className="h-6 w-24"
                        label={`${judge.title}: pass rate by day`}
                        points={summary.days.map((d) => d.pass_rate)}
                      />
                    ) : null}
                    <span className="font-numeral font-semibold tabular-nums">
                      {summary.scored === 0 ? none : formatPercent(summary.pass_rate)}
                    </span>
                  </div>
                </td>
                <td className="border-b border-line px-4 py-3 align-middle tabular-nums text-ink-soft">
                  {formatUsd(summary.cost_usd)}
                </td>
                <td className="border-b border-line px-4 py-3 align-middle">
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
