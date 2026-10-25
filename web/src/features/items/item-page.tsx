import { useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useParams } from '@tanstack/react-router';
import { ChevronLeft } from 'lucide-react';
import { type ReactNode, useMemo, useState } from 'react';
import { eventsQuery, itemQuery, runsQuery } from '../../api/queries';
import type { Item, Station } from '../../api/types';
import { factoryTitle, itemKey, useFactory, useFactoryParams } from '../../shared/factory';
import { humanize, plural } from '../../shared/format';
import { type ItemControls, ItemDecisions } from '../../shared/item-actions';
import { describeState, pausedNow, pullRequestsOf, type Tone } from '../../shared/item-state';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip, type ChipTone } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { PathCard, TimeCard } from './flow-cards';
import { NoteComposer } from './note-composer';
import { OutputsCard } from './outputs';
import { activeRun, buildTrace, latestToolCalls, noteRecipient, runLabel, type Trace } from './timeline';
import { TouchesCard } from './touches-card';
import { ToolRow, TraceList } from './trace-view';

/** The agent copy working the item now, as the trace names it. */
function agentOf(trace: Trace): string | null {
  const run = activeRun(trace);
  return run ? runLabel(run) : null;
}

const chipTone: Record<Tone, ChipTone> = { run: 'run', you: 'you', rework: 'rework', muted: 'plain', ok: 'ok' };

function TraceCard({
  item,
  trace,
  className,
  children,
}: {
  item: Item;
  trace: Trace;
  className?: string;
  children?: ReactNode;
}) {
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
      {children}
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
  const recipient = noteRecipient(trace, item.station);
  const station = factory.stations.find((s) => s.id === item.station);
  const state = describeState(
    { state: item.state, agent: agentOf(trace), rework_count: item.rework_count },
    { decides: holds(factory, 'item:decide') },
  );
  const decidable = {
    key,
    ref: item.ref,
    title: item.title,
    state: item.state,
    pull_requests: pullRequestsOf(
      item.outputs,
      factory.stations.map((s) => s.id),
    ),
  };
  const controls = {
    id: item.id,
    station: item.station,
    reworkTarget: item.flow?.rework_target ?? null,
    paused: pausedNow(item.state, events),
    factory,
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
            {item.type ? <Chip tone="plain">{humanize(item.type)}</Chip> : null}
            {item.lane ? <Chip tone="plain">{item.lane}</Chip> : null}
            <Chip tone="plain">At {humanize(item.station)}</Chip>
            <Chip tone={chipTone[state.tone]}>{state.text}</Chip>
            {item.touches && item.touches.count > 0 ? (
              <Chip tone="you" className="max-md:hidden">
                {plural(item.touches.count, 'human touch', 'human touches')}
              </Chip>
            ) : null}
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
          controls={controls}
          size="lg"
          className="hidden @[720px]:flex"
        />
      </header>

      <div className="flex flex-col gap-3 @[720px]:grid @[720px]:grid-cols-3 @[720px]:items-start @[720px]:gap-4">
        <div className="contents @[720px]:flex @[720px]:flex-col @[720px]:gap-4">
          <ItemDecisionsCard
            item={decidable}
            factory={factory.id}
            station={station}
            controls={controls}
            // A decision or a retry leads; pausing or moving work in flow comes after the trace.
            className={deciding || item.state === 'blocked' ? 'order-1' : 'order-9'}
          />
          <TimeCard item={item} factory={factory} trace={trace} className="order-2 @[720px]:order-none" />
          <TouchesCard
            item={item}
            itemKey={key}
            factory={factory}
            events={events}
            className="order-2 @[720px]:order-none"
          />
          <PathCard item={item} factory={factory} className="order-7 @[720px]:order-none" />
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
          >
            <NoteComposer
              itemKey={key}
              factory={factory}
              state={item.state}
              agent={recipient}
              className="mt-1 hidden @[720px]:flex"
            />
          </TraceCard>
        </div>
        <NoteComposer
          itemKey={key}
          factory={factory}
          state={item.state}
          agent={recipient}
          compact
          className="order-8 @[720px]:hidden"
        />
      </div>
    </>
  );
}

/** Phones: the decisions as full-width 44 px buttons right under the title. */
function ItemDecisionsCard({
  item,
  factory,
  station,
  controls,
  className,
}: {
  item: Parameters<typeof ItemDecisions>[0]['item'];
  factory: string;
  station: Station | undefined;
  controls: ItemControls;
  className: string;
}) {
  return (
    <ItemDecisions
      item={item}
      factory={factory}
      station={station}
      controls={controls}
      layout="stack"
      className={cn(className, 'rounded-card border border-line bg-card p-3.5 @[720px]:hidden')}
    />
  );
}
