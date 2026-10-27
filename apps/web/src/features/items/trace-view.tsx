import {
  Circle,
  CircleCheck,
  CircleX,
  FileText,
  type LucideIcon,
  MessageSquare,
  Pencil,
  Search,
  Terminal,
  Wrench,
} from 'lucide-react';
import { Fragment, type ReactNode, useId, useState } from 'react';
import type { ItemState } from '../../api/types';
import { formatClock, formatDuration, formatUsd, humanize } from '../../shared/format';
import { useModelLabel } from '../../shared/models';
import { Button } from '../../ui/button';
import { Chip, type ChipTone } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { outputLead } from './outputs';
import {
  type CheckEntry,
  type Entry,
  type NoteEntry,
  type Reply,
  type RunEntry,
  runCriteria,
  runRole,
  type ToolCall,
} from './timeline';

const shownTools = 6;

export function ToolRow({ call }: { call: ToolCall }) {
  return (
    <li
      className={cn(
        'grid gap-x-3 gap-y-1 rounded-control border bg-card px-3 py-2 text-sm md:grid-cols-[minmax(0,220px)_minmax(0,1fr)]',
        call.isError ? 'border-rework-tint' : 'border-line',
      )}
    >
      <span className="font-mono text-secondary [overflow-wrap:anywhere] text-run">
        {call.tool}
        {call.argument ? <span className="text-muted"> {call.argument}</span> : null}
      </span>
      <span className={cn('[overflow-wrap:anywhere]', call.isError && 'text-rework')}>
        {call.summary ?? <span className="text-muted">Waiting for the result…</span>}
      </span>
    </li>
  );
}

/** A tool's icon: reads, edits, shell commands, searches, the web; a wrench for the rest. */
function toolIcon(tool: string): LucideIcon {
  if (tool === 'Read') return FileText;
  if (tool === 'Edit' || tool === 'Write' || tool === 'MultiEdit') return Pencil;
  if (tool === 'Bash') return Terminal;
  if (tool === 'Grep' || tool === 'Glob') return Search;
  return Wrench;
}

/** A 22 px icon tile: plain for the agent's work, green and red for checks, violet for people. */
const tileTone = {
  plain: 'bg-panel text-muted',
  ok: 'bg-ok-tint text-ok-ink',
  rework: 'bg-rework-tint text-rework-ink',
  you: 'bg-you-tint text-you-ink',
} as const;
type TileTone = keyof typeof tileTone;

/** One row of a run's timeline: time, the icon tile, the text and a meta column on the right. */
function EventRow({
  at,
  tone,
  icon: Icon,
  children,
  meta,
}: {
  at: string;
  tone: TileTone;
  icon: LucideIcon;
  children: ReactNode;
  meta?: ReactNode;
}) {
  return (
    <li className="flex min-w-0 items-start gap-3 text-body">
      <time dateTime={at} className="w-11 shrink-0 pt-1 text-secondary text-muted tabular-nums">
        {formatClock(at)}
      </time>
      <span
        aria-hidden="true"
        className={cn('flex size-5.5 shrink-0 items-center justify-center rounded-chip', tileTone[tone])}
      >
        <Icon size={13} strokeWidth={1.75} />
      </span>
      <div className="min-w-0 flex-1 leading-normal [overflow-wrap:anywhere]">{children}</div>
      {meta ? <span className="shrink-0 pl-2 text-right text-secondary text-muted">{meta}</span> : null}
    </li>
  );
}

/** A run's outcome chip. The last run waits on a person when the item does. */
function outcomeOf(
  run: RunEntry,
  last: boolean,
  itemState: ItemState,
): { tone: ChipTone; label: string; dot: boolean } {
  if (run.status === 'running') {
    // A run with no end while the item no longer runs was cut short (a lease expired, goald restarted).
    return itemState === 'in_progress'
      ? { tone: 'run', label: 'Running', dot: true }
      : { tone: 'plain', label: 'Ended', dot: false };
  }
  if (last && itemState === 'waiting_human') return { tone: 'you', label: 'Waiting on you', dot: true };
  if (run.status === 'failed') return { tone: 'rework', label: 'Failed', dot: false };
  if (run.status === 'cancelled') return { tone: 'plain', label: 'Cancelled', dot: false };
  return { tone: 'ok', label: 'Finished', dot: false };
}

/** A run's rows: its tool calls, checks and notes, in the order they happened. */
type Row = { key: string; at: string; tone: TileTone; icon: LucideIcon; children: ReactNode; meta?: ReactNode };

function toolRow(call: ToolCall): Row {
  return {
    key: `tool-${call.at}-${call.id}`,
    at: call.at,
    tone: call.isError ? 'rework' : 'plain',
    icon: toolIcon(call.tool),
    children: (
      <>
        <span className="font-mono text-secondary">{call.tool}</span>
        {call.argument ? <span className="font-mono text-secondary text-muted"> {call.argument}</span> : null}
        {call.isError ? <span className="text-rework-ink"> failed</span> : null}
      </>
    ),
    meta: call.summary,
  };
}

function checkRow(check: CheckEntry): Row {
  const detail = [
    check.durationMs !== undefined ? formatDuration(check.durationMs) : '',
    !check.passed && check.exitCode !== undefined ? `exit ${check.exitCode}` : '',
  ]
    .filter(Boolean)
    .join(', ');
  return {
    key: check.key,
    at: check.at,
    tone: check.passed ? 'ok' : 'rework',
    icon: check.passed ? CircleCheck : CircleX,
    children: (
      <>
        <code className="font-mono text-secondary">{check.command}</code> {check.passed ? 'passed' : 'failed'}
      </>
    ),
    meta: detail || undefined,
  };
}

/** A note, or an event that says what a person did (violet), a sent-back or passed gate, or the engine's own step. */
function noteRow(note: NoteEntry): Row {
  const people = note.tone === 'you';
  const tone: TileTone = note.tone === 'ok' ? 'ok' : note.tone === 'rework' ? 'rework' : people ? 'you' : 'plain';
  return {
    key: note.key,
    at: note.at,
    tone,
    icon: people ? MessageSquare : Circle,
    children: (
      <>
        <span>{note.text}</span>
        {note.detail ? <span className="block text-muted">“{note.detail}”</span> : null}
      </>
    ),
  };
}

function ReplyLine({ reply }: { reply: Reply }) {
  if (reply.kind === 'text') return <p className="m-0 text-secondary leading-normal text-muted">“{reply.text}”</p>;
  const lead = outputLead(reply.value);
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <p className="m-0 text-secondary text-muted">Returned its output</p>
      {lead ? (
        <p className="m-0 text-body leading-normal">{lead}</p>
      ) : (
        <pre className="m-0 max-h-48 overflow-auto rounded-control bg-shell p-3 text-secondary leading-normal">
          {JSON.stringify(reply.value, null, 2)}
        </pre>
      )}
    </div>
  );
}

/** One run as a card: its station, role and outcome, then duration, turns and cost, its events. */
function RunCard({
  run,
  last,
  itemState,
  extra,
}: {
  run: RunEntry;
  last: boolean;
  itemState: ItemState;
  /** Checks and notes that happened while the run worked at its station. */
  extra: Entry[];
}) {
  const id = useId();
  const [all, setAll] = useState(false);
  const modelOf = useModelLabel();
  const outcome = outcomeOf(run, last, itemState);
  const label = runRole(run);
  const criteria = runCriteria(run);
  const rows: Row[] = [
    ...run.tools.map(toolRow),
    ...extra.flatMap((e): Row[] => (e.kind === 'check' ? [checkRow(e)] : e.kind === 'note' ? [noteRow(e)] : [])),
  ].sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
  const shown = all || rows.length <= shownTools ? rows : rows.slice(-shownTools);
  const duration = run.endedAt ? formatDuration(Date.parse(run.endedAt) - Date.parse(run.at)) : undefined;
  const meta = [
    run.turns !== undefined ? `${run.turns} ${run.turns === 1 ? 'turn' : 'turns'}` : undefined,
    run.costUsd !== undefined ? formatUsd(run.costUsd) : undefined,
  ].filter(Boolean);
  const version = run.version !== undefined ? `ran on v${run.version}` : undefined;

  return (
    <article aria-labelledby={id} className="flex min-w-0 flex-col rounded-card border border-line bg-card">
      <header className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line bg-panel px-4 py-3">
        <Chip tone="plain">{humanize(run.station)}</Chip>
        <h3 id={id} className="m-0 text-body font-medium">
          {label}
        </h3>
        <Chip tone={outcome.tone} dot={outcome.dot}>
          {outcome.label}
        </Chip>
        <span className="ml-auto flex flex-wrap gap-x-3 text-secondary text-muted tabular-nums">
          {duration ? <span>{duration}</span> : null}
          {meta.map((m) => (
            <span key={m}>{m}</span>
          ))}
        </span>
      </header>
      <div className="flex flex-col gap-3 p-4">
        {criteria.length > 0 ? (
          <div className="flex flex-col gap-2">
            <p className="m-0 text-secondary font-medium">Acceptance criteria</p>
            <ul className="m-0 flex list-none flex-col gap-2 p-0">
              {criteria.map((c) => (
                <li key={c} className="flex items-start gap-3 text-body leading-normal">
                  <span aria-hidden="true" className="mt-1 size-4 shrink-0 rounded-chip border border-rule" />
                  <span>{c}</span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
        {run.rules ? (
          <p className="m-0 text-secondary leading-normal text-muted">
            Ran with rules:{' '}
            {run.rules.map((rule, i) => (
              <Fragment key={rule.name}>
                {i > 0 ? ', ' : null}
                <span translate="no" className="font-mono text-meta break-all text-ink">
                  {rule.name}
                </span>
              </Fragment>
            ))}
          </p>
        ) : null}
        {rows.length > 0 ? (
          <ol aria-label={`Events by ${label}`} className="m-0 flex list-none flex-col gap-3 p-0">
            {shown.map((r) => (
              <EventRow key={r.key} at={r.at} tone={r.tone} icon={r.icon} meta={r.meta}>
                {r.children}
              </EventRow>
            ))}
          </ol>
        ) : null}
        {rows.length > shownTools ? (
          <Button
            variant="ghost"
            className="h-9 self-start px-2 text-secondary text-muted"
            onClick={() => setAll(!all)}
          >
            {all ? 'Show the latest only' : `Show all ${rows.length} events`}
          </Button>
        ) : null}
        {run.reply ? <ReplyLine reply={run.reply} /> : null}
        {run.error ? <p className="m-0 text-sm text-rework">{run.error}</p> : null}
        {version || run.model ? (
          <p className="m-0 text-meta text-muted">
            {[run.model ? modelOf(run.model) : undefined, version].filter(Boolean).join(', ')}
          </p>
        ) : null}
      </div>
    </article>
  );
}

/** The item's events outside a run (a note, a check the engine ran), as rows without a card. */
export function EventList({ entries }: { entries: Entry[] }) {
  return (
    <ol className="m-0 flex list-none flex-col gap-3 p-0">
      {entries.map((e) => {
        if (e.kind === 'run') return null;
        const r = e.kind === 'check' ? checkRow(e) : noteRow(e);
        return (
          <EventRow key={r.key} at={r.at} tone={r.tone} icon={r.icon} meta={r.meta}>
            {r.children}
          </EventRow>
        );
      })}
    </ol>
  );
}

export type TimelineStatus = 'loading' | 'error' | 'ready';

/**
 * The item's timeline: one card per run, with the checks and notes it worked through; events
 * before any run, and the events of a run that has no record, follow as rows.
 */
export function TraceList({
  entries,
  itemState,
  status = 'ready',
}: {
  entries: Entry[];
  itemState: ItemState;
  status?: TimelineStatus;
}) {
  if (status === 'loading') {
    return (
      <div aria-busy="true" className="min-h-24">
        <span className="sr-only">Loading</span>
      </div>
    );
  }
  if (entries.length === 0) {
    return <p className="m-0 text-body text-muted">Nothing has happened yet. The trace fills in as agents work.</p>;
  }

  const runs = entries.filter((e) => e.kind === 'run');
  const lastRun = runs.at(-1);
  // Each check or note belongs to the run that was last started before it; the others stand alone.
  const groups: { run?: RunEntry; extra: Entry[] }[] = [{ extra: [] }];
  for (const e of entries) {
    if (e.kind === 'run') groups.push({ run: e, extra: [] });
    else groups.at(-1)?.extra.push(e);
  }
  const [lead, ...rest] = groups;

  return (
    <div className="flex flex-col gap-4">
      {status === 'error' ? (
        <p role="alert" className="m-0 text-secondary text-rework-ink">
          Could not load the runs. The events are still listed.
        </p>
      ) : null}
      {lead && lead.extra.length > 0 ? <EventList entries={lead.extra} /> : null}
      {rest.map((g) =>
        g.run ? (
          <RunCard key={g.run.key} run={g.run} last={g.run === lastRun} itemState={itemState} extra={g.extra} />
        ) : null,
      )}
    </div>
  );
}
