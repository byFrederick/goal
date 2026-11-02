// The item's Trace tab (ADR 0019, decision 2; docs/design/screens/daily-work/trace.html): every run of the item,
// oldest first, on one time axis, each with its tool calls, checks, notes and human touches as rows. Selecting a
// row shows its detail in the side panel. On a phone the axis drops and each run is a list, the detail under
// the row. The data is the item's events and runs (`buildTrace`); new steps arrive when the item stream
// invalidates the item's queries, so nothing here polls.
import { type ReactNode, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { ItemState } from '../../api/types';
import { usePhone } from '../../app/phone-chrome';
import { formatClock, formatDuration, formatUsd, humanize, plural } from '../../shared/format';
import { useModelLabel } from '../../shared/models';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Disclosure } from '../../ui/disclosure';
import { Skeleton, SkeletonRows } from '../../ui/skeleton';
import { WorkingLine } from '../../ui/working-line';
import {
  activeRun,
  type CheckEntry,
  type Entry,
  type NoteEntry,
  type RunEntry,
  runRole,
  type ToolCall,
  type Trace,
} from './timeline';
import { checkStep } from './timeline-rows';
import { axisTicks, tickLabel } from './trace-axis';
import { outcomeOf, type TimelineStatus } from './trace-view';

type StepTone = 'run' | 'tool' | 'running' | 'ok' | 'failed' | 'you' | 'note';

type Step =
  | { kind: 'run'; key: string; ms: number; endMs: number | undefined; tone: StepTone; run: RunEntry }
  | { kind: 'tool'; key: string; ms: number; endMs: undefined; tone: StepTone; call: ToolCall; run: RunEntry }
  | { kind: 'check'; key: string; ms: number; endMs: number | undefined; tone: StepTone; check: CheckEntry }
  | { kind: 'note'; key: string; ms: number; endMs: undefined; tone: StepTone; note: NoteEntry };

interface Group {
  run?: RunEntry;
  steps: Step[];
}

const barTone: Record<StepTone, string> = {
  run: 'bg-ink',
  tool: 'bg-rule',
  running: 'bg-run',
  ok: 'bg-ok',
  failed: 'bg-rework',
  you: 'bg-you',
  note: 'bg-muted',
};

const legend: { label: string; tone: StepTone }[] = [
  { label: 'Run', tone: 'run' },
  { label: 'Tool call', tone: 'tool' },
  { label: 'Running', tone: 'running' },
  { label: 'Check passed', tone: 'ok' },
  { label: 'Failed', tone: 'failed' },
  { label: 'A person', tone: 'you' },
];

/** The waterfall's columns: the span, the time axis and the duration. */
const waterfallColumns = { gridTemplateColumns: 'minmax(0, 280px) minmax(0, 1fr) 72px' };

const ms = (at: string) => Date.parse(at);

/** The steps of each run, in the order they happened; events before any run come first, without a run row. */
function groupsOf(entries: Entry[]): Group[] {
  const groups: Group[] = [{ steps: [] }];
  for (const e of entries) {
    if (e.kind === 'run') {
      const tone: StepTone = e.status === 'running' ? 'running' : e.status === 'failed' ? 'failed' : 'run';
      groups.push({
        run: e,
        steps: [
          { kind: 'run', key: e.key, ms: ms(e.at), endMs: e.endedAt ? ms(e.endedAt) : undefined, tone, run: e },
          ...e.tools.map(
            (call): Step => ({
              kind: 'tool',
              key: `${e.key}:${call.at}-${call.id}`,
              ms: ms(call.at),
              endMs: undefined,
              tone: call.isError ? 'failed' : call.summary === undefined && e.status === 'running' ? 'running' : 'tool',
              call,
              run: e,
            }),
          ),
        ],
      });
    } else {
      const last = groups.at(-1);
      if (!last) continue;
      last.steps.push(
        e.kind === 'check'
          ? {
              kind: 'check',
              key: e.key,
              ms: ms(e.at),
              endMs: e.durationMs !== undefined ? ms(e.at) + e.durationMs : undefined,
              tone: e.passed ? 'ok' : 'failed',
              check: e,
            }
          : {
              kind: 'note',
              key: e.key,
              ms: ms(e.at),
              endMs: undefined,
              tone: e.tone === 'you' ? 'you' : e.tone === 'rework' ? 'failed' : e.tone === 'ok' ? 'ok' : 'note',
              note: e,
            },
      );
    }
  }
  // A run row stays first; the rest follow in time order.
  for (const g of groups) {
    const [head, ...rest] = g.steps;
    if (g.run && head) g.steps = [head, ...rest.sort((a, b) => a.ms - b.ms)];
    else g.steps.sort((a, b) => a.ms - b.ms);
  }
  return groups.filter((g) => g.run || g.steps.length > 0);
}

function label(step: Step): ReactNode {
  switch (step.kind) {
    case 'run':
      return (
        <>
          <Chip tone="plain">{humanize(step.run.station)}</Chip>
          <span title={runRole(step.run)} className="truncate font-medium">
            {runRole(step.run)}
          </span>
        </>
      );
    case 'tool':
      return (
        <>
          <span className="shrink-0 font-mono text-secondary text-ink">{step.call.tool}</span>
          {step.call.argument ? (
            <span title={step.call.argument} className="truncate font-mono text-secondary text-muted">
              {step.call.argument}
            </span>
          ) : null}
        </>
      );
    case 'check': {
      const text = checkStep(step.check)
        .segments.map((x) => x.text)
        .join('');
      return (
        <span title={text} className="truncate">
          {text}
        </span>
      );
    }
    case 'note':
      return (
        <span title={step.note.text} className="truncate">
          {step.note.text}
        </span>
      );
  }
}

/** How long a step took, as the right column says it: a run's and a check's duration. Tool calls report none. */
function tookOf(step: Step, now: number): string | undefined {
  if (step.kind === 'run') {
    return step.endMs === undefined
      ? `${formatDuration(Math.max(now - step.ms, 0))} so far`
      : formatDuration(step.endMs - step.ms);
  }
  if (step.kind === 'check' && step.endMs !== undefined) return formatDuration(step.endMs - step.ms);
  return undefined;
}

function Dot({ tone }: { tone: StepTone }) {
  return <i aria-hidden="true" className={cn('block size-1.5 flex-none rounded-full', barTone[tone])} />;
}

/** The selected step's detail: what it was, and when, how long and on what it ran. */
function StepDetail({ step, now, className }: { step: Step; now: number; className?: string }) {
  const modelOf = useModelLabel();
  const run = step.kind === 'run' || step.kind === 'tool' ? step.run : undefined;
  const took = tookOf(step, now);
  const failed = step.tone === 'failed';
  const output = step.kind === 'tool' ? step.call.summary : step.kind === 'run' ? step.run.error : undefined;
  const title =
    step.kind === 'tool'
      ? step.call.tool
      : step.kind === 'run'
        ? `${humanize(step.run.station)}, ${runRole(step.run)}`
        : step.kind === 'check'
          ? 'Check'
          : 'Note';
  const status =
    step.kind === 'tool'
      ? step.call.summary === undefined
        ? 'Waiting for the result'
        : step.call.isError
          ? 'Failed'
          : 'Done'
      : step.kind === 'run'
        ? outcomeOf(step.run, false, 'done').label
        : step.kind === 'check'
          ? step.check.passed
            ? 'Passed'
            : step.check.exitCode !== undefined
              ? `Exit ${step.check.exitCode}`
              : 'Failed'
          : undefined;
  const lead =
    step.kind === 'tool'
      ? step.call.argument
      : step.kind === 'check'
        ? step.check.command
        : step.kind === 'note'
          ? step.note.text
          : undefined;
  const rows: [string, string][] = [];
  if (run) rows.push(['Run', `${humanize(run.station)}, ${runRole(run)}`]);
  rows.push(['Started', formatClock(new Date(step.ms).toISOString())]);
  if (took) rows.push(['Took', took]);
  if (step.kind === 'run') {
    if (step.run.turns !== undefined) rows.push(['Turns', String(step.run.turns)]);
    if (step.run.costUsd !== undefined) rows.push(['Cost', formatUsd(step.run.costUsd)]);
  }
  if (run && (run.model || run.version !== undefined)) {
    rows.push([
      'Ran on',
      [run.model ? modelOf(run.model) : undefined, run.version !== undefined ? `v${run.version}` : undefined]
        .filter(Boolean)
        .join(', '),
    ]);
  }
  const id = useId();
  return (
    <aside aria-labelledby={id} className={cn('min-w-0 rounded-card border border-line bg-card', className)}>
      <header className="flex min-h-11 items-center gap-2 border-b border-line px-4 py-2">
        <h2
          id={id}
          title={title}
          className={cn('m-0 min-w-0 truncate text-body font-semibold', step.kind === 'tool' && 'font-mono')}
        >
          {title}
        </h2>
        {status ? (
          <span
            className={cn('ml-auto flex items-center gap-2 text-secondary', failed ? 'text-rework-ink' : 'text-muted')}
          >
            <Dot tone={step.tone} />
            {status}
          </span>
        ) : null}
      </header>
      <div className="flex flex-col gap-3 p-4">
        {lead ? <p className="m-0 font-mono text-secondary leading-normal [overflow-wrap:anywhere]">{lead}</p> : null}
        {step.kind === 'note' && step.note.detail ? (
          <p className="m-0 text-secondary text-muted">“{step.note.detail}”</p>
        ) : null}
        <dl style={{ gridTemplateColumns: '88px minmax(0, 1fr)' }} className="m-0 grid gap-x-3 gap-y-1 text-secondary">
          {rows.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-muted">{k}</dt>
              <dd className="m-0 [overflow-wrap:anywhere]">{v}</dd>
            </div>
          ))}
        </dl>
        {output ? (
          <Disclosure
            key={step.key}
            summary="Show the output"
            defaultOpen={failed}
            triggerClassName="min-h-7 w-fit justify-start px-0 text-secondary font-medium"
          >
            <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap wrap-break-word rounded-control border border-line bg-panel p-3 font-mono text-secondary leading-normal">
              {output}
            </pre>
          </Disclosure>
        ) : null}
      </div>
    </aside>
  );
}

function Legend() {
  return (
    <ul aria-label="Legend" className="m-0 flex list-none flex-wrap gap-x-4 gap-y-1 p-0 text-meta text-muted">
      {legend.map((l) => (
        <li key={l.label} className="inline-flex items-center gap-2">
          <i aria-hidden="true" className={cn('block h-1.5 w-4 rounded-chip', barTone[l.tone])} />
          {l.label}
        </li>
      ))}
    </ul>
  );
}

function Axis({ spanMs }: { spanMs: number }) {
  const track = useRef<HTMLSpanElement>(null);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const el = track.current;
    if (!el || typeof ResizeObserver === 'undefined') return;
    setWidth(el.clientWidth);
    const watch = new ResizeObserver(() => setWidth(el.clientWidth));
    watch.observe(el);
    return () => watch.disconnect();
  }, []);
  const ticks = width > 0 ? axisTicks(spanMs, width) : [0];
  return (
    <div
      aria-hidden="true"
      style={waterfallColumns}
      className="grid items-center border-b border-line bg-panel text-meta text-muted"
    >
      <span className="px-3 py-2">Span</span>
      <span ref={track} className="relative h-full min-h-7 overflow-hidden border-l border-line">
        {ticks.map((s) => (
          <span
            key={s}
            className="absolute inset-y-0 whitespace-nowrap border-l border-line pl-1 leading-7 first:border-l-0"
            style={{ left: `${((s * 1000) / spanMs) * 100}%` }}
          >
            {tickLabel(s)}
          </span>
        ))}
      </span>
      <span className="px-3 text-right">Took</span>
    </div>
  );
}

/** The keys of steps first seen while following; the first render and steps seen before never rise. */
function useRiseKeys(steps: Step[], following: boolean): Set<string> {
  const seen = useRef<Set<string> | null>(null);
  const [rise, setRise] = useState<Set<string>>(() => new Set());
  useLayoutEffect(() => {
    const known = seen.current;
    seen.current = new Set(steps.map((s) => s.key));
    if (!known || !following) return;
    const fresh = steps.filter((s) => !known.has(s.key)).map((s) => s.key);
    if (fresh.length > 0) setRise((was) => new Set([...was, ...fresh]));
  }, [steps, following]);
  return rise;
}

/**
 * While following, keeps the end of the trace in view and stops when the person scrolls up. Any scroll
 * container counts: scroll events do not bubble, so the capture phase on the document hears them all.
 * Our own scrolls only go down, so a decrease in scrollTop is the person.
 */
function useFollowScroll(
  end: { current: HTMLElement | null },
  following: boolean,
  trigger: string,
  watchSeq: number,
  onStop: (() => void) | undefined,
) {
  const lastSeq = useRef(watchSeq);
  // biome-ignore lint/correctness/useExhaustiveDependencies: `trigger` re-runs the scroll when a step arrives
  useEffect(() => {
    if (!following) return;
    const pressed = lastSeq.current !== watchSeq;
    lastSeq.current = watchSeq;
    const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;
    // A press jumps; steps that arrive afterwards glide, unless motion is reduced.
    end.current?.scrollIntoView({ block: 'end', behavior: pressed || reduced ? 'auto' : 'smooth' });
  }, [following, trigger, watchSeq, end]);
  const stop = useRef(onStop);
  stop.current = onStop;
  useEffect(() => {
    if (!following) return;
    const tops = new WeakMap<object, number>();
    const onScroll = (event: Event) => {
      const target = event.target;
      const el = target instanceof Document ? (target.scrollingElement ?? target.documentElement) : target;
      if (!(el instanceof Element)) return;
      const top = el.scrollTop;
      const before = tops.get(el);
      tops.set(el, top);
      if (before !== undefined && top < before - 1) stop.current?.();
    };
    document.addEventListener('scroll', onScroll, { capture: true, passive: true });
    return () => document.removeEventListener('scroll', onScroll, { capture: true });
  }, [following]);
}

/** The trace of an item: its runs and their steps, and the live line of the run that is working. */
export function TraceTab({
  trace,
  itemState,
  status = 'ready',
  now = Date.now(),
  step,
  following = false,
  watchSeq = 0,
  onStopFollowing,
}: {
  trace: Trace;
  itemState: ItemState;
  status?: TimelineStatus;
  now?: number;
  /** An event id: the step it drew (`ev-<id>`) is selected first, as the Teardown's "In the trace" link asks. */
  step?: number | undefined;
  /** Watch live is on: new steps are kept in view and rise in. */
  following?: boolean;
  /** Counts presses of Watch live; each one scrolls to the newest step again. */
  watchSeq?: number;
  /** Called when the person scrolls up. */
  onStopFollowing?: () => void;
}) {
  const phone = usePhone();
  const groups = useMemo(() => groupsOf(trace.entries), [trace.entries]);
  const steps = useMemo(() => groups.flatMap((g) => g.steps), [groups]);
  const [selected, setSelected] = useState<string | undefined>(step === undefined ? undefined : `ev-${step}`);
  const run = itemState === 'in_progress' ? activeRun(trace) : undefined;
  const lastRun = trace.entries.findLast((e) => e.kind === 'run');

  const t0 = steps.reduce((m, s) => Math.min(m, s.ms), Number.POSITIVE_INFINITY);
  const latest = steps.reduce((m, s) => Math.max(m, s.ms, s.endMs ?? 0), 0);
  const t1 = run ? Math.max(latest, now) : latest;
  const spanMs = Math.max(t1 - t0, 1000);
  const runs = trace.entries.filter((e) => e.kind === 'run').length;

  const current = steps.find((s) => s.key === selected) ?? steps.at(-1);
  // A phone's row closes when tapped again; a wide screen always has a step selected.
  const pick = (key: string) => setSelected((was) => (phone && was === key ? undefined : key));

  const end = useRef<HTMLDivElement>(null);
  const newestKey = steps.at(-1)?.key;
  const rise = useRiseKeys(steps, following);
  useFollowScroll(end, following, `${watchSeq}:${newestKey ?? ''}`, watchSeq, onStopFollowing);

  const placeholder = <SkeletonRows rows={6} />;
  const ready = status !== 'loading';

  return (
    <div className="flex flex-col gap-4">
      <p aria-live="polite" className="m-0 inline-flex min-h-5 items-center gap-2 text-sm text-link">
        {run ? (
          <>
            <span aria-hidden="true" className="size-2 rounded-full bg-run ring-4 ring-run-tint" />
            <WorkingLine text={`${runRole(run)} working, ${run.tools.length} tool calls`} running />
          </>
        ) : null}
      </p>
      <Skeleton ready={ready} placeholder={placeholder}>
        {steps.length === 0 ? (
          <p className="m-0 text-body text-muted">No tool calls yet.</p>
        ) : (
          <div className="flex flex-col gap-4">
            {status === 'error' ? (
              <p role="alert" className="m-0 text-secondary text-rework-ink">
                Could not load the runs. The events are still listed.
              </p>
            ) : null}
            <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
              <Legend />
              <p className="m-0 text-secondary text-muted tabular-nums">
                {formatDuration(spanMs)}, {plural(runs, 'run')}
                {trace.costUsd > 0 ? `, ${formatUsd(trace.costUsd)}` : ''}
              </p>
            </div>
            <div className="grid grid-cols-[minmax(0,1fr)] items-start gap-4 @[900px]:grid-cols-[minmax(0,1fr)_340px]">
              <div
                className={cn(
                  'flex min-w-0 flex-col',
                  phone ? 'gap-3' : 'overflow-hidden rounded-card border border-line bg-card',
                )}
              >
                {phone ? null : <Axis spanMs={spanMs} />}
                {groups.map((g) => (
                  <section
                    key={g.run?.key ?? 'before'}
                    aria-label={g.run ? `${humanize(g.run.station)}, ${runRole(g.run)}` : 'Before the first run'}
                    className={cn(phone && 'overflow-hidden rounded-card border border-line bg-card')}
                  >
                    <ol className="m-0 flex list-none flex-col p-0">
                      {g.steps.map((s) => (
                        <StepRow
                          key={s.key}
                          step={s}
                          phone={phone}
                          t0={t0}
                          spanMs={spanMs}
                          t1={t1}
                          now={now}
                          selected={s.key === current?.key}
                          open={s.key === selected}
                          onSelect={pick}
                          last={g.run === lastRun}
                          itemState={itemState}
                          rising={rise.has(s.key)}
                        />
                      ))}
                    </ol>
                  </section>
                ))}
                {/* Where following scrolls to: just past the newest step. */}
                <div ref={end} aria-hidden="true" className="h-px" />
              </div>
              {phone || !current ? null : (
                <StepDetail step={current} now={now} className="@[900px]:sticky @[900px]:top-4" />
              )}
            </div>
          </div>
        )}
      </Skeleton>
    </div>
  );
}

function StepRow({
  step,
  phone,
  t0,
  t1,
  spanMs,
  now,
  selected,
  open,
  onSelect,
  last,
  itemState,
  rising,
}: {
  step: Step;
  phone: boolean;
  t0: number;
  t1: number;
  spanMs: number;
  now: number;
  selected: boolean;
  /** Phones: the row's detail is open under it. */
  open: boolean;
  onSelect: (key: string) => void;
  last: boolean;
  itemState: ItemState;
  /** The step arrived while following: it rises in (MOTION.md, a new item in a list). */
  rising: boolean;
}) {
  const isRun = step.kind === 'run';
  const took = tookOf(step, now);
  const outcome = step.kind === 'run' ? outcomeOf(step.run, last, itemState) : undefined;
  const left = ((step.ms - t0) / spanMs) * 100;
  const end = step.kind === 'run' && step.endMs === undefined ? t1 : step.endMs;
  const width = end !== undefined ? Math.max(((end - step.ms) / spanMs) * 100, 0) : 0;
  const button = (
    <button
      type="button"
      {...(phone ? { 'aria-expanded': open } : { 'aria-pressed': selected })}
      onClick={() => onSelect(step.key)}
      className={cn(
        'w-full cursor-pointer border-0 text-left text-body transition-colors duration-(--duration-quick) hover:bg-hover',
        selected ? 'bg-run-tint' : isRun ? 'bg-panel' : 'bg-transparent',
        phone ? 'flex min-h-11 items-center gap-3 px-3 py-2' : 'grid min-h-8 items-center p-0',
      )}
      style={phone ? undefined : waterfallColumns}
    >
      {phone ? (
        <>
          {isRun ? null : <Dot tone={step.tone} />}
          <span className="flex min-w-0 flex-1 items-center gap-2">{label(step)}</span>
          {outcome ? <span className="text-secondary text-muted">{outcome.label}</span> : null}
          <time dateTime={new Date(step.ms).toISOString()} className="font-mono text-meta text-muted tabular-nums">
            {formatClock(new Date(step.ms).toISOString())}
          </time>
        </>
      ) : (
        <>
          <span className={cn('flex min-w-0 items-center gap-2 px-3', !isRun && 'pl-8')}>
            {label(step)}
            {outcome ? <span className="text-secondary text-muted">{outcome.label}</span> : null}
          </span>
          <span aria-hidden="true" className="relative h-full min-h-8 self-stretch border-l border-line">
            <i
              className={cn(
                'absolute top-1/2 block -translate-y-1/2 rounded-chip',
                isRun ? 'h-1' : 'h-1.5',
                barTone[step.tone],
              )}
              style={{ left: `min(${left}%, calc(100% - 4px))`, width: `max(${width}%, 4px)` }}
            />
          </span>
          <span className="px-3 text-right text-secondary text-muted tabular-nums">{took}</span>
        </>
      )}
    </button>
  );
  return (
    <li
      className={cn(
        'border-t border-line first:border-t-0',
        rising && 'motion-safe:animate-row-in motion-reduce:animate-fade-in',
      )}
    >
      {button}
      {phone && open ? <StepDetail step={step} now={now} className="m-2" /> : null}
    </li>
  );
}
