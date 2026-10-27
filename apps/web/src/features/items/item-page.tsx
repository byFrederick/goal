import { useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useParams, useSearch } from '@tanstack/react-router';
import { ChevronLeft, Eye, LayoutGrid } from 'lucide-react';
import { useMemo, useRef, useState } from 'react';
import { eventsQuery, inboxQuery, itemQuery, itemRunsQuery } from '../../api/queries';
import type { Station } from '../../api/types';
import { usePhone } from '../../app/phone-chrome';
import { TopBarSlot } from '../../app/top-bar';
import { factoryTitle, itemKey, refParam, useFactory, useFactoryParams } from '../../shared/factory';
import { type ItemControls, ItemDecisions } from '../../shared/item-actions';
import { pausedNow, pullRequestsOf } from '../../shared/item-state';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { NotAvailable } from '../../ui/not-available';
import { Stepper } from '../../ui/stepper';
import { ItemTabs } from './item-tabs';
import { NoteComposer } from './note-composer';
import { OutputsCard } from './outputs';
import { QuestionCard } from './question-card';
import { CostCard, DetailsCard, OverlapsCard, RulesCard } from './side-cards';
import { noFlow, stepsOf } from './steps';
import { activeRun, buildTrace, criteriaOf, editedFiles, latestToolCalls, runRole, type Trace } from './timeline';
import { TouchesCard } from './touches-card';
import { EventList, type TimelineStatus, ToolRow, TraceList } from './trace-view';

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
        className="h-11 self-start px-2 text-body underline underline-offset-3"
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
  const runsQ = useQuery(itemRunsQuery(org.id, key));
  const runs = runsQ.data;
  const trace = useMemo(() => buildTrace(events, runs?.runs), [events, runs]);
  const [fullTrace, setFullTrace] = useState(false);
  const { tab = 'timeline' } = useSearch({ from: '/o/$org/f/$factory/items/$ref' });
  const noteField = useRef<HTMLTextAreaElement>(null);
  const compactNoteField = useRef<HTMLTextAreaElement>(null);
  // The wide field is hidden on phones and the compact one elsewhere, so the focus goes to the one shown.
  const focusNote = () => (noteField.current?.offsetParent ? noteField.current : compactNoteField.current)?.focus();
  // The question: this item's open decision in the inbox.
  const { data: inbox } = useQuery(inboxQuery(org.id, factory.id));
  const question = inbox?.entries.find((e) => e.kind === 'decision' && e.item?.id === item.id);
  const latest = trace.entries.findLast((e) => e.kind === 'run');
  const runsStatus: TimelineStatus = runsQ.isPending ? 'loading' : runsQ.isError ? 'error' : 'ready';
  const files = editedFiles(trace).length;
  const checks = trace.entries.filter((e) => e.kind === 'check');
  const run = item.state === 'in_progress' ? activeRun(trace) : undefined;
  const station = factory.stations.find((s) => s.id === item.station);
  const decidable = useMemo(
    () => ({
      key,
      ref: item.ref,
      title: item.title,
      state: item.state,
      pull_requests: pullRequestsOf(
        item.outputs,
        factory.stations.map((s) => s.id),
      ),
    }),
    [key, item, factory],
  );
  const controls = useMemo<ItemControls>(
    () => ({
      id: item.id,
      station: item.station,
      reworkTarget: item.flow?.rework_target ?? null,
      paused: pausedNow(item.state, events),
      factory,
    }),
    [item, events, factory],
  );
  const steps = stepsOf({ item, stations: factory.stations, events, flow: item.flow ?? noFlow, now: Date.now() });
  // The top bar's actions: Watch live (not built yet), then the item's decisions, Move in a menu.
  // A phone has its decisions in its card under the title, so the bar has no actions there.
  const phone = usePhone();
  const actions = useMemo(
    () =>
      phone ? undefined : (
        <div className="flex items-center gap-2">
          <NotAvailable>
            <Button variant="secondary">
              <Eye size={16} strokeWidth={1.75} aria-hidden="true" />
              Watch live
            </Button>
          </NotAvailable>
          <ItemDecisions item={decidable} factory={factory.id} station={station} controls={controls} moveInMenu />
        </div>
      ),
    [phone, decidable, factory.id, station, controls],
  );
  const board = `${factoryTitle(factory)} board`;
  // At a decision, what is being decided on comes before how it was made.
  const deciding = item.state === 'waiting_human';

  return (
    <>
      <TopBarSlot crumb={`#${item.number}`} actions={actions} />
      <header className="flex flex-col gap-3">
        <Link
          to="/o/$org/f/$factory/board"
          params={params}
          className="inline-flex min-h-11 items-center gap-2 self-start text-body text-muted no-underline md:hidden"
        >
          <ChevronLeft size={16} strokeWidth={1.75} aria-hidden="true" />
          {board}
        </Link>
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-secondary font-semibold text-muted">#{item.number}</span>
          {item.class_of_service === 'expedite' ? <Chip tone="rework">Expedite</Chip> : null}
          <Chip tone="plain">{item.type}</Chip>
          {item.lane ? <Chip tone="plain">{item.lane}</Chip> : null}
          {item.assembly ? (
            <Link
              to="/o/$org/f/$factory/items/$ref"
              params={{ ...params, ref: refParam(item.assembly.ref) }}
              className="no-underline hover:text-run"
            >
              <Chip tone="plain">
                <LayoutGrid size={12} strokeWidth={1.75} aria-hidden="true" />
                {item.assembly.title}, part {item.assembly.part} of {item.assembly.parts}
              </Chip>
            </Link>
          ) : null}
        </div>
        <h1 className="m-0 text-page-title tracking-display text-balance">{item.title}</h1>
        <Stepper label="Progress through the line" steps={steps} waiting={item.state === 'waiting_human'} />
      </header>

      <div className="flex flex-col gap-3 @[720px]:grid @[720px]:grid-cols-3 @[720px]:items-start @[720px]:gap-4">
        <div className="contents @[720px]:flex @[720px]:flex-col @[720px]:gap-4">
          {/* Only on a phone: the top bar holds these actions from 768 px, so they never show twice. */}
          {phone ? (
            <ItemDecisionsCard
              item={decidable}
              factory={factory.id}
              station={station}
              controls={controls}
              // A decision or a retry leads; pausing or moving work in flow comes after the trace.
              className={deciding || item.state === 'blocked' ? 'order-1' : 'order-9'}
            />
          ) : null}
          <DetailsCard
            item={item}
            factory={factory}
            events={events}
            trace={trace}
            className="order-2 @[720px]:order-none"
          />
          <CostCard item={item} factory={factory} runs={runs?.runs ?? []} className="order-2 @[720px]:order-none" />
          <OverlapsCard item={item} className="order-2 @[720px]:order-none" />
          <RulesCard item={item} factory={factory} trace={trace} className="order-2 @[720px]:order-none" />
          <TouchesCard
            item={item}
            itemKey={key}
            factory={factory}
            events={events}
            className="order-2 @[720px]:order-none"
          />
        </div>
        <div className="contents @[720px]:col-span-2 @[720px]:flex @[720px]:flex-col @[720px]:gap-4">
          {question ? (
            <QuestionCard
              entry={question}
              agent={latest ? runRole(latest) : null}
              paused={controls.paused}
              onWriteAnswer={focusNote}
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
              <p aria-live="polite" className="m-0 inline-flex items-center gap-2 text-sm text-run">
                {run ? (
                  <>
                    <span aria-hidden="true" className="size-2 rounded-full bg-run ring-4 ring-run-tint" />
                    {runRole(run)} working, {run.tools.length} tool calls
                  </>
                ) : null}
              </p>
              <TraceList entries={trace.entries} itemState={item.state} status={runsStatus} />
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
          <NoteComposer
            itemKey={key}
            factory={factory}
            state={item.state}
            fieldRef={noteField}
            className="hidden @[720px]:flex"
          />
        </div>
        <NoteComposer
          itemKey={key}
          factory={factory}
          state={item.state}
          compact
          fieldRef={compactNoteField}
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
      className={cn(className, 'rounded-card border border-line bg-card p-4')}
    />
  );
}
