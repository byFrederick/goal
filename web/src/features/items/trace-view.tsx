import { CircleCheck, CircleX } from 'lucide-react';
import { useState } from 'react';
import type { ItemState } from '../../api/types';
import { formatClock, formatDuration, formatUsd, humanize } from '../../shared/format';
import { toneText } from '../../shared/item-state';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import type { CheckEntry, Entry, NoteEntry, RunEntry, ToolCall } from './timeline';

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
    run.model,
    run.turns !== undefined ? `${run.turns} turns` : undefined,
    run.costUsd !== undefined ? formatUsd(run.costUsd) : undefined,
    run.endedAt ? formatDuration(Date.parse(run.endedAt) - Date.parse(run.at)) : undefined,
  ].filter(Boolean);

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <p className="m-0 text-[15px] leading-snug">
          <strong className="font-medium">{run.agent}</strong> worked at {humanize(run.station)}
        </p>
        {chip}
      </div>
      {meta.length > 0 ? <p className="m-0 text-[13px] text-muted">{meta.join(', ')}</p> : null}
      {run.tools.length > 0 ? (
        <ol className="m-0 flex list-none flex-col gap-1.5 p-0" aria-label={`Tool calls by ${run.agent}`}>
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
      {run.message ? <p className="m-0 text-sm leading-normal text-muted">“{run.message}”</p> : null}
      {run.error ? <p className="m-0 text-sm text-rework">{run.error}</p> : null}
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
