import { useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useParams, useSearch } from '@tanstack/react-router';
import { ChevronLeft } from 'lucide-react';
import { useMemo, useRef, useState } from 'react';
import { eventsQuery, inboxQuery, itemQuery, runsQuery } from '../../api/queries';
import type { Station } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { factoryTitle, itemKey, useFactory, useFactoryParams } from '../../shared/factory';
import { humanize, plural } from '../../shared/format';
import { type ItemControls, ItemDecisions } from '../../shared/item-actions';
import { describeState, pausedNow, pullRequestsOf } from '../../shared/item-state';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { WaitingFor } from '../../shared/waiting-for';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { PathCard, TimeCard } from './flow-cards';
import { ItemStateChip } from './item-state-chip';
import { ItemTabs } from './item-tabs';
import { NoteComposer } from './note-composer';
import { OutputsCard } from './outputs';
import { QuestionCard } from './question-card';
import { activeRun, buildTrace, criteriaOf, editedFiles, latestToolCalls, runRole, type Trace } from './timeline';
import { TouchesCard } from './touches-card';
import { EventList, type TimelineStatus, ToolRow, TraceList } from './trace-view';

/** The role working the item now, as the trace names it. */
function roleOf(trace: Trace): string | null {
  const run = activeRun(trace);
  return run ? runRole(run) : null;
}

/** The spec's acceptance criteria, as the Spec tab lists them. */
function SpecCriteria({ criteria }: { criteria: string[] }) {
  return (
    <SectionCard title="Acceptance criteria">
      {criteria.length > 0 ? (
        <ul className="m-0 flex list-none flex-col gap-2 p-0">
          {criteria.map((c) => (
            <li key={c} className="flex items-start gap-3 text-body leading-normal">
              <span aria-hidden="true" className="mt-1 size-4 shrink-0 rounded-chip border border-rule" />
              <span>{c}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="m-0 text-body text-muted">No acceptance criteria yet. The spec adds them when it is written.</p>
      )}
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
        <ol className="m-0 flex list-none flex-col gap-2 p-0">
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
  const runsQ = useQuery(runsQuery(org.id, key));
  const runs = runsQ.data;
  const trace = useMemo(() => buildTrace(events, runs?.runs), [events, runs]);
  const [fullTrace, setFullTrace] = useState(false);
  const { tab = 'timeline' } = useSearch({ from: '/o/$org/f/$factory/items/$ref' });
  const noteField = useRef<HTMLTextAreaElement>(null);
  // The question: this item's open decision in the inbox, when the caller may decide it.
  const { data: inbox } = useQuery(inboxQuery(org.id, factory.id));
  const question = inbox?.entries.find((e) => e.kind === 'decision' && e.item?.id === item.id);
  const latest = trace.entries.findLast((e) => e.kind === 'run');
  const runsStatus: TimelineStatus = runsQ.isPending ? 'loading' : runsQ.isError ? 'error' : 'ready';
  const files = editedFiles(trace).length;
  const checks = trace.entries.filter((e) => e.kind === 'check');
  const station = factory.stations.find((s) => s.id === item.station);
  const state = describeState(
    { state: item.state, agent: roleOf(trace), rework_count: item.rework_count },
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
      <TopBarSlot crumb={`#${item.number}`} />
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex max-w-[820px] min-w-0 flex-col gap-3">
          <Link
            to="/o/$org/f/$factory/board"
            params={params}
            className="inline-flex min-h-11 items-center gap-2 self-start text-[15px] text-muted no-underline md:hidden"
          >
            <ChevronLeft size={16} strokeWidth={1.75} aria-hidden="true" />
            {board}
          </Link>
          <nav aria-label="Breadcrumb" className="hidden text-sm text-muted md:block">
            <ol className="m-0 flex list-none gap-2 p-0">
              <li>
                <Link to="/o/$org/f/$factory/board" params={params} className="text-muted hover:text-run">
                  {board}
                </Link>
              </li>
              <li aria-hidden="true">/</li>
              <li aria-current="page">{item.ref}</li>
            </ol>
          </nav>
          <div className="flex flex-wrap gap-2 md:order-last">
            {item.class_of_service === 'expedite' ? <Chip tone="rework">Expedite</Chip> : null}
            {item.type ? <Chip tone="plain">{humanize(item.type)}</Chip> : null}
            {item.lane ? <Chip tone="plain">{item.lane}</Chip> : null}
            <Chip tone="plain">At {humanize(item.station)}</Chip>
            <ItemStateChip state={item.state} label={state} waiting={item.waiting ?? null} />
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

      <WaitingFor waiting={item.waiting} />

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
          {question ? (
            <QuestionCard
              entry={question}
              agent={latest ? runRole(latest) : null}
              paused={controls.paused}
              onWriteAnswer={() => noteField.current?.focus()}
              className="order-2 @[720px]:order-none"
            />
          ) : null}
          {fullTrace ? null : (
            <LatestSteps
              trace={trace}
              onShowAll={() => setFullTrace(true)}
              className={cn(deciding ? 'order-4' : 'order-3', '@[720px]:hidden')}
            />
          )}
          <ItemTabs tab={tab} files={files} checks={checks.length} className="order-5 @[720px]:order-none" />
          {tab === 'timeline' ? (
            <section
              aria-label="Timeline"
              className={cn('order-6 flex flex-col gap-4 @[720px]:order-none', !fullTrace && 'hidden @[720px]:flex')}
            >
              <TraceList entries={trace.entries} itemState={item.state} status={runsStatus} />
              <NoteComposer
                itemKey={key}
                factory={factory}
                state={item.state}
                fieldRef={noteField}
                className="hidden @[720px]:flex"
              />
            </section>
          ) : null}
          {tab === 'spec' ? (
            <div className="order-6 flex flex-col gap-4 @[720px]:order-none">
              {item.body ? (
                <SectionCard title="Request">
                  <p className="m-0 text-body leading-relaxed whitespace-pre-wrap">{item.body}</p>
                </SectionCard>
              ) : null}
              <SpecCriteria criteria={criteriaOf(item.outputs.spec)} />
            </div>
          ) : null}
          {tab === 'changes' ? (
            <div className="order-6 flex flex-col gap-4 @[720px]:order-none">
              {Object.keys(item.outputs).length > 0 ? (
                <OutputsCard factory={factory} outputs={item.outputs} />
              ) : (
                <p className="m-0 text-body text-muted">Nothing has been produced yet.</p>
              )}
            </div>
          ) : null}
          {tab === 'checks' ? (
            <SectionCard title="Checks" className="order-6 @[720px]:order-none">
              {checks.length > 0 ? (
                <EventList entries={checks} />
              ) : (
                <p className="m-0 text-body text-muted">No checks have run yet.</p>
              )}
            </SectionCard>
          ) : null}
        </div>
        <NoteComposer itemKey={key} factory={factory} state={item.state} compact className="order-8 @[720px]:hidden" />
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
      className={cn(className, 'rounded-card border border-line bg-card p-4 @[720px]:hidden')}
    />
  );
}
