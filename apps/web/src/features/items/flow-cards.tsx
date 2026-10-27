// The Item screen's time and path cards, drawn from the item's flow (Item.flow): its spans at
// each station, its lead-time target, the task limits and the run in progress (Item and
// Mobile-Item boards).
import { ExternalLink } from 'lucide-react';
import type { CurrentRun, Factory, Item, ItemFlow, ItemSpan, Station } from '../../api/types';
import { formatClock, formatDuration, formatUsd, humanize } from '../../shared/format';
import { describeState, pullRequestLabel, pullRequestsOf } from '../../shared/item-state';
import { useModelLabel } from '../../shared/models';
import { holds } from '../../shared/permissions';
import { SectionCard, StatRow } from '../../ui/card';
import { cn } from '../../ui/cn';
import { Hatch } from '../../ui/hatch';
import { formatTarget, type TimeBar, timeBar, toneLabel } from './time';
import type { Trace } from './timeline';

const noFlow: ItemFlow = {
  spans: [],
  target_seconds: null,
  rework_target: null,
  budget: { per_item_usd: null, max_turns_per_task: null },
  current_run: null,
};

function useNow(): number {
  // Ages are recomputed on every render; renders follow the stream, which is often enough.
  return Date.now();
}

/** "$1.12 of $4.00" against the task budget, else the spend alone. */
function spendText(item: Item, flow: ItemFlow): string {
  const cap = flow.budget.per_item_usd;
  return cap ? `${formatUsd(item.cost_usd)} of ${formatUsd(cap)}` : formatUsd(item.cost_usd);
}

function turnsText(run: CurrentRun, flow: ItemFlow): string {
  const max = flow.budget.max_turns_per_task;
  return max ? `${run.turn} of ${max}` : String(run.turn);
}

/** What the bar says to a screen reader: each station's time by state, then the target. */
function barLabel(bar: TimeBar, spans: readonly ItemSpan[]): string {
  const parts = bar.stations.map(({ station, ms }) => {
    const kinds = bar.segments
      .filter((s) => s.station === station)
      .map((s) => `${formatDuration(s.ms)} ${toneLabel[s.tone].toLowerCase()}`);
    return `${humanize(station)} ${formatDuration(ms)}${kinds.length > 1 ? ` (${kinds.join(', ')})` : ''}`;
  });
  if (spans.length === 0) parts.push('no time per station recorded');
  if (bar.leftMs !== null) parts.push(`${formatDuration(bar.leftMs)} left of the target`);
  if (bar.overMs !== null) parts.push(`${formatDuration(bar.overMs)} over the target`);
  return parts.join(', ');
}

export function TimeCard({
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
  const now = useNow();
  const modelOf = useModelLabel();
  const flow = item.flow ?? noFlow;
  const end = item.done_at ?? new Date(now).toISOString();
  const bar = timeBar(flow.spans, { createdAt: item.created_at, end, targetSeconds: flow.target_seconds });
  const run = flow.current_run;
  const model = run?.model ?? trace.model;
  const expedite = item.class_of_service === 'expedite';
  const target = flow.target_seconds ? formatTarget(flow.target_seconds) : null;
  const prs = pullRequestsOf(
    item.outputs,
    factory.stations.map((s) => s.id),
  );
  const width = (ms: number) => `${(ms / bar.scaleMs) * 100}%`;

  return (
    <SectionCard
      title={item.done_at ? `Done in ${formatDuration(bar.elapsedMs)}` : `${formatDuration(bar.elapsedMs)} so far`}
      description={
        target ? (
          <span className="hidden @[720px]:inline">
            {expedite ? 'Expedite target' : 'Lead-time target'}: within {target}
          </span>
        ) : (
          <span className="hidden @[720px]:inline">Time at each station</span>
        )
      }
      aside={
        flow.target_seconds ? (
          <span className="shrink-0 text-[13px] text-muted @[720px]:hidden">
            target {formatTarget(flow.target_seconds, 'short')}
          </span>
        ) : null
      }
      className={className}
    >
      <div role="img" aria-label={barLabel(bar, flow.spans)} className="relative flex h-3 gap-[3px]">
        {bar.segments.map((s, i) => (
          <div
            key={s.key}
            className={cn(
              'h-full min-w-[3px] shrink overflow-hidden',
              i === 0 && 'rounded-l-[3px]',
              i === bar.segments.length - 1 && bar.leftMs === null && 'rounded-r-[3px]',
            )}
            style={{ flexBasis: width(s.ms) }}
          >
            <Hatch tone={s.tone} />
          </div>
        ))}
        {bar.leftMs !== null && bar.leftMs > 0 ? (
          <div
            className={cn(
              'h-full min-w-[3px] grow rounded-r-[3px] border border-line bg-card',
              bar.segments.length === 0 && 'rounded-l-[3px]',
            )}
          />
        ) : null}
        {bar.overMs !== null && bar.targetMs !== null ? (
          <span className="absolute -top-1 h-5 w-0.5 rounded-[1px] bg-ink" style={{ left: width(bar.targetMs) }} />
        ) : null}
      </div>
      <div aria-hidden="true" className="-mt-1 flex flex-wrap gap-x-4 gap-y-1 text-[13px] text-muted tabular-nums">
        {bar.stations.map((s) => (
          <span key={s.station}>
            {humanize(s.station)} {formatDuration(s.ms)}
          </span>
        ))}
        {bar.overMs !== null ? (
          <span className="ml-auto text-rework">{formatDuration(bar.overMs)} over target</span>
        ) : bar.targetMs !== null ? (
          <span className="ml-auto">{formatTarget(bar.targetMs / 1000, 'short')}</span>
        ) : null}
      </div>
      {bar.tones.length > 1 ? (
        <ul className="m-0 -mt-1.5 flex list-none flex-wrap gap-x-3.5 gap-y-1 p-0 text-[13px] text-muted">
          {bar.tones.map((tone) => (
            <li key={tone} className="inline-flex items-center gap-1.5">
              <span aria-hidden="true" className="size-2.5 overflow-hidden rounded-[2px]">
                <Hatch tone={tone} />
              </span>
              {toneLabel[tone]}
            </li>
          ))}
        </ul>
      ) : null}
      <p className="m-0 text-[13px] text-muted @[720px]:hidden">
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
            <span className="flex flex-col items-end gap-0.5">
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

/**
 * What an unreached station without a description does. The done station is its name alone: a
 * note there ("Finished") would read as the item's state.
 */
const kindNote: Record<Station['kind'], string | null> = {
  queue: 'Waits to be pulled',
  agent: 'Agents work here',
  human: 'Approvers decide here',
  done: null,
};

/** An item's visits to one station: consecutive spans there, with the time spent. */
function visitsAt(spans: readonly ItemSpan[], station: string, end: number) {
  let visits = 0;
  let spent = 0;
  let first: string | undefined;
  spans.forEach((s, i) => {
    if (s.station !== station) return;
    if (spans[i - 1]?.station !== station) visits++;
    first ??= s.entered_at;
    spent += Math.max(0, (s.left_at ? Date.parse(s.left_at) : end) - Date.parse(s.entered_at));
  });
  return { visits, spent, first };
}

export function PathCard({ item, factory, className }: { item: Item; factory: Factory; className?: string }) {
  const now = useNow();
  const flow = item.flow ?? noFlow;
  const end = item.done_at ? Date.parse(item.done_at) : now;
  const run = flow.current_run;
  const state = describeState(
    { state: item.state, agent: run?.agent ?? null, rework_count: item.rework_count },
    { decides: holds(factory, 'item:decide') },
  );

  return (
    <SectionCard title="Path" className={className}>
      <ol className="m-0 flex list-none flex-col p-0">
        {factory.stations.map((s) => {
          const { visits, spent, first } = visitsAt(flow.spans, s.id, end);
          const current = s.id === item.station;
          const visited = visits > 0 || current;
          const note = current
            ? state.text
            : visited
              ? `Spent ${formatDuration(spent)}`
              : (s.description ?? kindNote[s.kind]);
          const dot = current
            ? {
                run: 'bg-run ring-run-tint',
                you: 'bg-you ring-you-tint',
                rework: 'bg-rework ring-rework-tint',
                muted: 'bg-ink ring-line',
                ok: 'bg-ok ring-line',
              }[state.tone]
            : visited
              ? 'bg-ink ring-line'
              : 'bg-card ring-[1.5px] ring-muted';
          return (
            <li
              key={s.id}
              aria-current={current ? 'step' : undefined}
              className="grid grid-cols-[22px_minmax(0,1fr)_auto] items-start gap-3 py-[7px]"
            >
              <span aria-hidden="true" className={cn('mt-1 ml-[5px] size-3 rounded-full ring-4', dot)} />
              <span>
                <span className="block text-[15px] font-medium">{humanize(s.id)}</span>
                {note ? (
                  <span className="mt-0.5 block text-[13px] text-muted">
                    {note}
                    {visits > 1 ? `, ${visits} visits` : ''}
                  </span>
                ) : null}
              </span>
              <span className="text-[13px] text-muted tabular-nums">{first ? formatClock(first) : ''}</span>
            </li>
          );
        })}
      </ol>
    </SectionCard>
  );
}
