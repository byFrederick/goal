import { CircleCheck, CircleX } from 'lucide-react';
import { useState } from 'react';
import type { ItemState } from '../../api/types';
import { formatClock, formatDuration, formatUsd, humanize } from '../../shared/format';
import { toneText } from '../../shared/item-state';
import { useModelLabel } from '../../shared/models';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { outputLead } from './outputs';
import {
  type CheckEntry,
  type Entry,
  type NoteEntry,
  type Reply,
  type RunEntry,
  runRole,
  type ToolCall,
} from './timeline';

const shownTools = 6;

export function ToolRow({ call }: { call: ToolCall }) {
  return (
    <li
      className={cn(
        'grid gap-x-3 gap-y-0.5 rounded-control border bg-surface px-3 py-2 text-sm md:grid-cols-[minmax(0,220px)_minmax(0,1fr)]',
        call.isError ? 'border-rework-line' : 'border-hairline',
      )}
    >
      <span className="font-mono text-[13px] [overflow-wrap:anywhere] text-run">
        {call.tool}
        {call.argument ? <span className="text-muted"> {call.argument}</span> : null}
      </span>
      <span className={cn('[overflow-wrap:anywhere]', call.isError && 'text-rework')}>
        {call.summary ?? <span className="text-muted">Waiting for the result…</span>}
      </span>
    </li>
  );
}

function RunBlock({ run, itemState }: { run: RunEntry; itemState: ItemState }) {
  const [all, setAll] = useState(false);
  const modelOf = useModelLabel();
  // A run with no end while the item no longer runs was cut short (a lease expired, goald restarted).
  const status = run.status === 'running' && itemState !== 'in_progress' ? 'ended' : run.status;
  const chip = {
    running: <Chip tone="run">Running</Chip>,
    succeeded: <Chip tone="ok">Finished</Chip>,
    failed: <Chip tone="rework">Failed</Chip>,
    cancelled: <Chip tone="plain">Cancelled</Chip>,
    ended: <Chip tone="plain">Ended</Chip>,
  }[status];
  const tools = all ? run.tools : run.tools.slice(-shownTools);
  const meta = [
    run.model ? modelOf(run.model) : undefined,
    run.turns !== undefined ? `${run.turns} ${run.turns === 1 ? 'turn' : 'turns'}` : undefined,
    run.costUsd !== undefined ? formatUsd(run.costUsd) : undefined,
    run.endedAt ? formatDuration(Date.parse(run.endedAt) - Date.parse(run.at)) : undefined,
    run.version !== undefined ? `ran on v${run.version}` : undefined,
  ].filter(Boolean);
  const label = runRole(run);
  const metaText = meta.join(', ');

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <p className="m-0 text-[15px] leading-snug">
          <strong className="font-medium">{label}</strong> worked at {humanize(run.station)}
        </p>
        {chip}
      </div>
      {metaText ? (
        <p className="m-0 truncate text-[13px] text-muted" title={metaText}>
          {metaText}
        </p>
      ) : null}
      {run.tools.length > 0 ? (
        <ol className="m-0 flex list-none flex-col gap-1.5 p-0" aria-label={`Tool calls by ${label}`}>
          {tools.map((t) => (
            <ToolRow key={`${t.at}-${t.id}`} call={t} />
          ))}
        </ol>
      ) : null}
      {run.tools.length > shownTools ? (
        <Button variant="ghost" className="h-9 self-start px-2 text-[13px] text-muted" onClick={() => setAll(!all)}>
          {all ? 'Show the latest only' : `Show all ${run.tools.length} tool calls`}
        </Button>
      ) : null}
      {run.reply ? <ReplyLine reply={run.reply} /> : null}
      {run.error ? <p className="m-0 text-sm text-rework">{run.error}</p> : null}
    </div>
  );
}

/**
 * The agent's last reply: prose in quotes; the output it returned (a JSON reply) by its lead
 * prose, else formatted, never as a quoted JSON string.
 */
function ReplyLine({ reply }: { reply: Reply }) {
  if (reply.kind === 'text') return <p className="m-0 text-sm leading-normal text-muted">“{reply.text}”</p>;
  const lead = outputLead(reply.value);
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <p className="m-0 text-[13px] text-muted">Returned its output</p>
      {lead ? (
        <p className="m-0 text-sm leading-normal">{lead}</p>
      ) : (
        <pre className="m-0 max-h-48 overflow-auto rounded-control bg-shell p-3 text-[13px] leading-normal">
          {JSON.stringify(reply.value, null, 2)}
        </pre>
      )}
    </div>
  );
}

function CheckLine({ check }: { check: CheckEntry }) {
  const Icon = check.passed ? CircleCheck : CircleX;
  return (
    <p className="m-0 flex flex-wrap items-center gap-x-2 gap-y-1 text-[15px] leading-snug">
      <Icon size={16} strokeWidth={1.75} aria-hidden="true" className={check.passed ? 'text-ok' : 'text-rework'} />
      <span>{check.passed ? 'Check passed' : 'Check failed'}</span>
      <code className="rounded-chip bg-surface px-1.5 py-0.5 text-[13px]">{check.command}</code>
      <span className="text-[13px] text-muted">
        {[
          check.durationMs !== undefined ? formatDuration(check.durationMs) : '',
          !check.passed && check.exitCode !== undefined ? `exit ${check.exitCode}` : '',
        ]
          .filter(Boolean)
          .join(', ')}
      </span>
    </p>
  );
}

function NoteLine({ note }: { note: NoteEntry }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <p className={cn('m-0 text-[15px] leading-snug', note.tone !== 'muted' && toneText[note.tone])}>{note.text}</p>
      {note.detail ? (
        <p className="m-0 rounded-control border border-line bg-surface px-3 py-2 text-sm leading-normal">
          {note.detail}
        </p>
      ) : null}
    </div>
  );
}

/** The item's history, oldest first, as the approved trace layout: time, then what happened. */
export function TraceList({ entries, itemState }: { entries: Entry[]; itemState: ItemState }) {
  if (entries.length === 0) {
    return <p className="m-0 text-[15px] text-muted">Nothing has happened yet. The trace fills in as agents work.</p>;
  }
  return (
    <ol className="m-0 flex list-none flex-col gap-4 p-0">
      {entries.map((e) => (
        <li
          key={e.key}
          className="grid grid-cols-[44px_minmax(0,1fr)] gap-3 md:grid-cols-[52px_minmax(0,1fr)] md:gap-3.5"
        >
          <time dateTime={e.at} className="pt-0.5 text-[13px] text-muted tabular-nums">
            {formatClock(e.at)}
          </time>
          {e.kind === 'run' ? (
            <RunBlock run={e} itemState={itemState} />
          ) : e.kind === 'check' ? (
            <CheckLine check={e} />
          ) : (
            <NoteLine note={e} />
          )}
        </li>
      ))}
    </ol>
  );
}
