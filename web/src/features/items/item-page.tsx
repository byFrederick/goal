import { useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useParams } from '@tanstack/react-router';
import { ChevronLeft, ExternalLink } from 'lucide-react';
import { useMemo, useState } from 'react';
import { eventsQuery, itemQuery, runsQuery } from '../../api/queries';
import type { Factory, Item, Station, StationKind } from '../../api/types';
import { factoryTitle, itemKey, stationKind, useFactory, useFactoryParams } from '../../shared/factory';
import { formatClock, formatDuration, formatUsd, humanize } from '../../shared/format';
import { ItemDecisions } from '../../shared/item-actions';
import { describeState, pullRequestLabel, pullRequestOf, type Tone } from '../../shared/item-state';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { SectionCard, StatRow } from '../../ui/card';
import { Chip, type ChipTone } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Hatch, type HatchTone } from '../../ui/hatch';
import { OutputsCard } from './outputs';
import { activeRun, buildTrace, latestToolCalls, runLabel, type StationVisit, type Trace } from './timeline';
import { ToolRow, TraceList } from './trace-view';

/** The agent copy working the item now, as the trace names it. */
function agentOf(trace: Trace): string | null {
  const run = activeRun(trace);
  return run ? runLabel(run) : null;
}

const chipTone: Record<Tone, ChipTone> = { run: 'run', you: 'you', rework: 'rework', muted: 'plain', ok: 'ok' };

function useNow(): number {
  // Ages are recomputed on every render; renders follow the stream, which is often enough.
  return Date.now();
}

/** Agents' time teal, time waiting on a person violet, queue time gray; a blocked present rust. */
function visitTone(item: Item, kind: StationKind | undefined, current: boolean): HatchTone {
  if (current && item.state === 'blocked') return 'rework';
  if (kind === 'agent') return 'run';
  if (kind === 'human') return 'you';
  return 'target';
}

function TimeCard({
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
  const end = item.done_at ? Date.parse(item.done_at) : now;
  const total = Math.max(1, end - Date.parse(item.created_at));
  const visits: StationVisit[] =
    trace.visits.length > 0 ? trace.visits : [{ station: item.station, enteredAt: item.created_at }];
  const spans = visits.map((v, i) => ({
    visit: v,
    ms: Math.max(0, (v.leftAt ? Date.parse(v.leftAt) : end) - Date.parse(v.enteredAt)),
    current: i === visits.length - 1 && !item.done_at,
  }));
  const byStation = new Map<string, number>();
  for (const s of spans) byStation.set(s.visit.station, (byStation.get(s.visit.station) ?? 0) + s.ms);
  const pr = pullRequestOf(
    item.outputs,
    factory.stations.map((s) => s.id),
  );

  return (
    <SectionCard
      title={item.done_at ? `Done in ${formatDuration(total)}` : `${formatDuration(total)} so far`}
      description="Time at each station"
      className={className}
    >
      <div
        role="img"
        aria-label={[...byStation].map(([s, ms]) => `${humanize(s)} ${formatDuration(ms)}`).join(', ')}
        className="flex h-3 gap-[3px]"
      >
        {spans.map((s, i) => (
          <div
            key={`${s.visit.station}-${s.visit.enteredAt}`}
            className={cn(
              'h-full min-w-[3px] overflow-hidden',
              i === 0 && 'rounded-l-[3px]',
              i === spans.length - 1 && 'rounded-r-[3px]',
            )}
            style={{ width: `${(s.ms / total) * 100}%` }}
          >
            <Hatch tone={visitTone(item, stationKind(factory, s.visit.station), s.current)} />
          </div>
        ))}
      </div>
      <ul aria-hidden="true" className="m-0 -mt-1 flex list-none flex-wrap gap-x-4 gap-y-1 p-0 text-[13px] text-muted">
        {[...byStation].map(([s, ms]) => (
          <li key={s}>
            {humanize(s)} {formatDuration(ms)}
          </li>
        ))}
      </ul>
      <div className="flex flex-col">
        <StatRow label="Spend">{formatUsd(item.cost_usd)}</StatRow>
        <StatRow label="Turns">{trace.turns > 0 ? trace.turns : 'None finished yet'}</StatRow>
        {trace.model ? (
          <StatRow label="Model">
            <span className="normal-nums">{trace.model}</span>
          </StatRow>
        ) : null}
        <StatRow label="Sent back">
          <span className={cn(item.rework_count > 0 && 'text-rework')}>
            {item.rework_count === 0 ? 'Never' : `${item.rework_count}×`}
          </span>
        </StatRow>
        {pr ? (
          <StatRow label="Change">
            <a href={pr} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 hover:text-run">
              {pullRequestLabel(pr)}
              <ExternalLink size={14} strokeWidth={1.5} aria-hidden="true" />
              <span className="sr-only">(opens in a new tab)</span>
            </a>
          </StatRow>
        ) : null}
        <StatRow label="Created">{formatClock(item.created_at)}</StatRow>
      </div>
    </SectionCard>
  );
}

const kindNote = {
  queue: 'Waits to be pulled',
  agent: 'Agents work here',
  human: 'You decide here',
  done: 'Finished',
} as const;

function PathCard({
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
  const state = describeState({
    state: item.state,
    agent: agentOf(trace),
    rework_count: item.rework_count,
  });

  return (
    <SectionCard title="Path" className={className}>
      <ol className="m-0 flex list-none flex-col p-0">
        {factory.stations.map((s) => {
          const visits = trace.visits.filter((v) => v.station === s.id);
          const current = s.id === item.station;
          const visited = visits.length > 0 || current;
          const spent = visits.reduce(
            (n, v) =>
              n +
              (v.leftAt ? Date.parse(v.leftAt) : item.done_at ? Date.parse(item.done_at) : now) -
              Date.parse(v.enteredAt),
            0,
          );
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
              : 'bg-surface ring-[1.5px] ring-faint';
          return (
            <li
              key={s.id}
              aria-current={current ? 'step' : undefined}
              className="grid grid-cols-[22px_minmax(0,1fr)_auto] items-start gap-3 py-[7px]"
            >
              <span aria-hidden="true" className={cn('mt-1 ml-[5px] size-3 rounded-full ring-4', dot)} />
              <span>
                <span className="block text-[15px] font-medium">{humanize(s.id)}</span>
                <span className="mt-0.5 block text-[13px] text-muted">
                  {current ? state.text : visited ? `Spent ${formatDuration(spent)}` : kindNote[s.kind]}
                  {visits.length > 1 ? `, ${visits.length} visits` : ''}
                </span>
              </span>
              <span className="text-[13px] text-muted tabular-nums">
                {visits[0] ? formatClock(visits[0].enteredAt) : ''}
              </span>
            </li>
          );
        })}
      </ol>
    </SectionCard>
  );
}

function TraceCard({ item, trace, className }: { item: Item; trace: Trace; className?: string }) {
  const run = item.state === 'in_progress' ? activeRun(trace) : undefined;
  return (
    <SectionCard
      title="Trace"
      className={className}
      aside={
        <span aria-live="polite" className="inline-flex items-center gap-2 text-sm text-run">
          {run ? (
            <>
              <span
                aria-hidden="true"
                className="size-2 rounded-full bg-run shadow-[0_0_0_4px_var(--color-run-tint)]"
              />
              {runLabel(run)} working, {run.tools.length} tool calls
            </>
          ) : null}
        </span>
      }
    >
      <TraceList entries={trace.entries} itemState={item.state} />
    </SectionCard>
  );
}

function LatestSteps({ trace, onShowAll, className }: { trace: Trace; onShowAll: () => void; className?: string }) {
  const steps = latestToolCalls(trace, 4);
  return (
    <SectionCard title="Latest steps" className={className}>
      {steps.length === 0 ? (
        <p className="m-0 text-sm text-muted">No tool calls yet.</p>
      ) : (
        <ol className="m-0 flex list-none flex-col gap-1.5 p-0">
          {steps.map((t) => (
            <ToolRow key={`${t.at}-${t.id}`} call={t} />
          ))}
        </ol>
      )}
      <Button
        variant="ghost"
        className="h-11 self-start px-2 text-[15px] underline underline-offset-3"
        onClick={onShowAll}
      >
        Full trace
      </Button>
    </SectionCard>
  );
}

export function ItemPage() {
  const factory = useFactory();
  const client = useQueryClient();
  const params = useFactoryParams();
  const { ref } = useParams({ from: '/o/$org/f/$factory/items/$ref' });
  const key = itemKey(factory.name, ref);
  const org = useOrg();
  const { data: item } = useSuspenseQuery(itemQuery(org.id, key));
  const { data: events } = useSuspenseQuery(eventsQuery(client, org.id, key));
  // The run records refine the trace when they arrive; the events alone already draw it.
  const { data: runs } = useQuery(runsQuery(org.id, key));
  const trace = useMemo(() => buildTrace(events, runs?.runs), [events, runs]);
  const [fullTrace, setFullTrace] = useState(false);
  const station = factory.stations.find((s) => s.id === item.station);
  const state = describeState({
    state: item.state,
    agent: agentOf(trace),
    rework_count: item.rework_count,
  });
  const decidable = {
    key,
    ref: item.ref,
    title: item.title,
    state: item.state,
    pr_url: pullRequestOf(
      item.outputs,
      factory.stations.map((s) => s.id),
    ),
  };
  const board = `${factoryTitle(factory)} board`;
  // At a decision, what is being decided on comes before how it was made.
  const deciding = item.state === 'waiting_human';

  return (
    <>
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex max-w-[820px] min-w-0 flex-col gap-2.5">
          <Link
            to="/o/$org/f/$factory/board"
            params={params}
            className="inline-flex min-h-11 items-center gap-1.5 self-start text-[15px] text-muted no-underline md:hidden"
          >
            <ChevronLeft size={16} strokeWidth={1.75} aria-hidden="true" />
            {board}
          </Link>
          <nav aria-label="Breadcrumb" className="hidden text-sm text-muted md:block">
            <ol className="m-0 flex list-none gap-1.5 p-0">
              <li>
                <Link to="/o/$org/f/$factory/board" params={params} className="text-muted hover:text-run">
                  {board}
                </Link>
              </li>
              <li aria-hidden="true">/</li>
              <li aria-current="page">{item.ref}</li>
            </ol>
          </nav>
          <div className="flex flex-wrap gap-1.5 md:order-last">
            {item.class_of_service === 'expedite' ? <Chip tone="rework">Expedite</Chip> : null}
            <Chip tone="plain">{humanize(item.type)}</Chip>
            {item.lane ? <Chip tone="plain">{item.lane}</Chip> : null}
            <Chip tone="plain">At {humanize(item.station)}</Chip>
            <Chip tone={chipTone[state.tone]}>{state.text}</Chip>
            <Chip tone="plain" className="md:hidden">
              {item.ref}
            </Chip>
          </div>
          <h1 className="m-0 text-[26px] leading-[1.15] font-normal tracking-display text-balance md:text-[42px] md:leading-[1.08]">
            {item.title}
          </h1>
        </div>
        <ItemDecisions
          item={decidable}
          factory={factory.id}
          station={station}
          size="lg"
          className="hidden @[720px]:flex"
        />
      </header>

      <div className="flex flex-col gap-3 @[720px]:grid @[720px]:grid-cols-3 @[720px]:items-start @[720px]:gap-4">
        <div className="contents @[720px]:flex @[720px]:flex-col @[720px]:gap-4">
          <ItemDecisionsCard item={decidable} factory={factory.id} station={station} />
          <TimeCard item={item} factory={factory} trace={trace} className="order-2 @[720px]:order-none" />
          <PathCard item={item} factory={factory} trace={trace} className="order-7 @[720px]:order-none" />
        </div>
        <div className="contents @[720px]:col-span-2 @[720px]:flex @[720px]:flex-col @[720px]:gap-4">
          {fullTrace ? null : (
            <LatestSteps
              trace={trace}
              onShowAll={() => setFullTrace(true)}
              className={cn(deciding ? 'order-4' : 'order-3', '@[720px]:hidden')}
            />
          )}
          <OutputsCard
            factory={factory}
            outputs={item.outputs}
            className={cn(deciding ? 'order-3' : 'order-4', '@[720px]:order-none')}
          />
          {item.body ? (
            <SectionCard title="Request" className="order-5 @[720px]:order-none">
              <p className="m-0 text-[15px] leading-relaxed whitespace-pre-wrap">{item.body}</p>
            </SectionCard>
          ) : null}
          <TraceCard
            item={item}
            trace={trace}
            className={cn('order-6 @[720px]:order-none', !fullTrace && 'hidden @[720px]:flex')}
          />
        </div>
      </div>
    </>
  );
}

/** Phones: the decisions as full-width 44 px buttons right under the title. */
function ItemDecisionsCard({
  item,
  factory,
  station,
}: {
  item: Parameters<typeof ItemDecisions>[0]['item'];
  factory: string;
  station: Station | undefined;
}) {
  return (
    <ItemDecisions
      item={item}
      factory={factory}
      station={station}
      layout="stack"
      className="order-1 rounded-card border border-line bg-card p-3.5 @[720px]:hidden"
    />
  );
}
