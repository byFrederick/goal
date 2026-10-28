// The Cases table: each case's name and description, the agent that runs it, what checks it, its last
// three tries as dots, what its tries cost and a Run for that one case. A wide table scrolls sideways
// inside its card; the page never does.
import { ChartColumn, Check, FileText, type LucideIcon } from 'lucide-react';
import type { EvalCaseEntry, EvalCheckedBy } from '../../api/types';
import { formatUsd } from '../../shared/format';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { caseLabel, checkedByText, dotsOf, none, type TryTone, triesLabel } from './evals-format';

const checkedByIcon: Record<EvalCheckedBy['kind'], LucideIcon> = {
  contract: Check,
  judge: ChartColumn,
  reference_patch: FileText,
};

const dotTone: Record<TryTone, string> = {
  pass: 'bg-ok',
  fail: 'bg-rework',
  open: 'bg-hover',
};

export function EvalsTable({
  cases,
  canRun,
  running,
  onRun,
}: {
  cases: readonly EvalCaseEntry[];
  /** definition:edit: a Run is offered only to those who may start one. */
  canRun: boolean;
  /** A run is being started: no second one until it is. */
  running: boolean;
  onRun: (name: string) => void;
}) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-xl border-collapse text-secondary">
        <thead>
          <tr className="text-left text-meta text-muted">
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-semibold">
              Case
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-semibold">
              Agent
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-semibold">
              Checked by
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-semibold">
              Last 3 tries
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2 font-control font-semibold">
              Cost
            </th>
            <th scope="col" className="border-b border-line px-4 pb-2">
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {cases.map((entry) => {
            const Icon = checkedByIcon[entry.checked_by.kind];
            const dots = dotsOf(entry.last_tries);
            return (
              <tr key={entry.name} className="hover:bg-panel">
                <td className="border-b border-line px-4 py-3 align-middle">
                  <div className="flex min-w-0 flex-col gap-1">
                    <span className="font-mono text-secondary text-ink">{entry.name}</span>
                    <span className="text-meta text-muted">{entry.description}</span>
                  </div>
                </td>
                <td className="border-b border-line px-4 py-3 align-middle">
                  <Chip>{entry.agent}</Chip>
                </td>
                <td className="border-b border-line px-4 py-3 align-middle">
                  <span className="inline-flex items-center gap-2 whitespace-nowrap text-secondary text-ink-soft">
                    <Icon aria-hidden="true" size={14} strokeWidth={1.75} className="shrink-0 text-muted" />
                    {checkedByText(entry.checked_by)}
                  </span>
                </td>
                <td className="border-b border-line px-4 py-3 align-middle">
                  {dots.length === 0 ? (
                    <span className="text-muted">{none}</span>
                  ) : (
                    <span role="img" aria-label={triesLabel(entry.last_tries)} className="inline-flex gap-1">
                      {dots.map((tone, i) => (
                        <span
                          // biome-ignore lint/suspicious/noArrayIndexKey: a fixed row of at most three dots, by place only
                          key={`${entry.name}-${i}`}
                          aria-hidden="true"
                          className={cn('size-2 rounded-full', dotTone[tone])}
                        />
                      ))}
                    </span>
                  )}
                </td>
                <td className="border-b border-line px-4 py-3 align-middle tabular-nums text-ink-soft">
                  {entry.cost_usd === undefined ? none : formatUsd(entry.cost_usd)}
                </td>
                <td className="border-b border-line px-4 py-3 align-middle">
                  <Button
                    size="sm"
                    variant="secondary"
                    aria-label={caseLabel(entry)}
                    disabled={!canRun || running}
                    onClick={() => onRun(entry.name)}
                  >
                    Run
                  </Button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
