// The item page's run and spend rows that the stepper does not carry: the turns and model of the run
// in progress (Item.flow), the spend against the budget, the send-backs and the pull requests. The time
// at each station is the stepper's (steps.ts). These rows move to the side cards of the item page (a later part of the assembly).
import { ExternalLink } from 'lucide-react';
import type { CurrentRun, Factory, Item, ItemFlow } from '../../api/types';
import { formatUsd, humanize } from '../../shared/format';
import { pullRequestLabel, pullRequestsOf } from '../../shared/item-state';
import { useModelLabel } from '../../shared/models';
import { SectionCard, StatRow } from '../../ui/card';
import { cn } from '../../ui/cn';
import { noFlow } from './steps';
import type { Trace } from './timeline';

/** "$1.12 of $4.00" against the task budget, else the spend alone. */
function spendText(item: Item, flow: ItemFlow): string {
  const cap = flow.budget.per_item_usd;
  return cap ? `${formatUsd(item.cost_usd)} of ${formatUsd(cap)}` : formatUsd(item.cost_usd);
}

function turnsText(run: CurrentRun, flow: ItemFlow): string {
  const max = flow.budget.max_turns_per_task;
  return max ? `${run.turn} of ${max}` : String(run.turn);
}

export function RunCard({
  item,
  factory,
  trace,
  className,
}: {
  item: Item;
  factory: Factory;
  trace: Trace;
  className?: string;
}) {
  const modelOf = useModelLabel();
  const flow = item.flow ?? noFlow;
  const run = flow.current_run;
  const model = run?.model ?? trace.model;
  const prs = pullRequestsOf(
    item.outputs,
    factory.stations.map((s) => s.id),
  );

  return (
    <SectionCard title="Details" className={className}>
      <p className="m-0 text-secondary text-muted @[720px]:hidden">
        {run ? `${humanize(run.agent)} on turn ${turnsText(run, flow)}, ` : ''}
        {spendText(item, flow)}
      </p>
      <div className="hidden flex-col @[720px]:flex">
        <StatRow label="Spend">{spendText(item, flow)}</StatRow>
        <StatRow label="Turns">
          {run ? turnsText(run, flow) : trace.turns > 0 ? trace.turns : 'None finished yet'}
        </StatRow>
        {model ? (
          <StatRow label="Model">
            <span className="min-w-0 truncate normal-nums" title={model}>
              {modelOf(model)}
            </span>
          </StatRow>
        ) : null}
        {run?.output_contract ? (
          <StatRow label="Must return">
            <span className="font-mono text-sm">{run.output_contract}</span>
          </StatRow>
        ) : null}
        <StatRow label="Sent back">
          <span className={cn(item.rework_count > 0 && 'text-rework')}>
            {item.rework_count === 0 ? 'Never' : `${item.rework_count}×`}
          </span>
        </StatRow>
        {prs.length > 0 ? (
          <StatRow label={prs.length === 1 ? 'Change' : 'Changes'}>
            <span className="flex flex-col items-end gap-1">
              {prs.map((pr) => (
                <a
                  key={pr}
                  href={pr}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 hover:text-run"
                >
                  {pullRequestLabel(pr, prs.length > 1)}
                  <ExternalLink size={14} strokeWidth={1.5} aria-hidden="true" />
                  <span className="sr-only">(opens in a new tab)</span>
                </a>
              ))}
            </span>
          </StatRow>
        ) : null}
      </div>
    </SectionCard>
  );
}
