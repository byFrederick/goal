// The Check block of a trigger with `check` (ADR 0040 decision 19; the picture is
// docs/design/screens/intake/trigger-check.jpg): the agent that looks at the source, what it can use, the
// brief it follows and what it remembers from one check to the next. The block takes a trigger value and
// an onChange and fetches nothing: the page hands it the memory it read, so New trigger can mount it too.
import { useState } from 'react';
import type { TriggerCheckMemory } from '../../api/types';
import type { Trigger } from '../../shared/definition/model';
import { Button } from '../../ui/button';
import { Input, NativeSelect, Textarea } from '../../ui/field';
import { InfoTip } from '../jonah/info-tip';
import { Block } from './form-parts';
import type { BoardChoices } from './trigger-yaml';

const checkedUpTo = (at: string) => new Date(at).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });

export interface CheckSectionProps {
  id: string;
  value: Trigger;
  onChange: (next: Trigger) => void;
  board: BoardChoices;
  problems?: string[] | undefined;
  /** What the saved trigger remembers; absent for a trigger not saved yet. */
  memory?: TriggerCheckMemory | undefined;
  /** Forgets the saved state; throws the server's words when it cannot (409 still checking). */
  onClear?: (() => Promise<void>) | undefined;
}

export function CheckSection({ id, value, onChange, board, problems, memory, onClear }: CheckSectionProps) {
  const check = value.check ?? { agent: '', brief: '' };
  const set = (patch: Partial<typeof check>) => onChange({ ...value, check: { ...check, ...patch } });
  const agents =
    board.agents.some((a) => a.name === check.agent) || !check.agent
      ? board.agents
      : [...board.agents, { name: check.agent, tools: [] }];
  const tools = agents.find((a) => a.name === check.agent)?.tools ?? [];
  const briefId = `${id}-brief`;
  return (
    <Block
      id={id}
      title="Check"
      info="An agent looks at the source with its environment’s tools, and decides for each thing it finds: file an item, add a note to the open item that covers it, or skip it. It reads code but never writes, and stops after 5 minutes."
      problems={problems}
    >
      <div className="flex flex-wrap items-center gap-2 text-body">
        <span>Ask</span>
        <NativeSelect
          aria-label="Agent"
          value={check.agent}
          onChange={(e) => set({ agent: e.target.value })}
          className="h-8 rounded-control border-rule bg-card px-2 text-body font-control"
        >
          {check.agent ? null : <option value="">Choose an agent</option>}
          {agents.map((a) => (
            <option key={a.name} value={a.name}>
              {a.name}
            </option>
          ))}
        </NativeSelect>
        {tools.length ? (
          <>
            <span>with</span>
            <ul aria-label="Tools" className="m-0 flex list-none flex-wrap gap-2 p-0">
              {tools.map((tool) => (
                <li
                  key={tool}
                  className="rounded-chip border border-line bg-panel px-2 py-1 font-mono text-meta text-ink"
                >
                  {tool}
                </li>
              ))}
            </ul>
          </>
        ) : null}
      </div>
      <div className="flex flex-col gap-2">
        <span className="flex items-center gap-1 text-secondary font-semibold">
          <label htmlFor={briefId}>Brief</label>
          <InfoTip label="About the brief">
            Text it reads from the source reaches the agent marked as untrusted data, never as instructions. A path to a
            Markdown file in the definition, such as ./triggers/name.md, works too.
          </InfoTip>
        </span>
        <Textarea
          id={briefId}
          rows={4}
          value={check.brief}
          onChange={(e) => set({ brief: e.target.value })}
          className="resize-none rounded-cell border-rule bg-card px-3 py-2 text-body"
        />
      </div>
      <Remembers memory={memory} onClear={onClear} />
    </Block>
  );
}

function Remembers({
  memory,
  onClear,
}: {
  memory?: TriggerCheckMemory | undefined;
  onClear?: (() => Promise<void>) | undefined;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const clear = async () => {
    if (!onClear) return;
    setBusy(true);
    setError(null);
    try {
      await onClear();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Try again in a moment.');
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-col gap-2">
      <span className="flex items-center gap-1 text-secondary font-semibold">
        Remembers
        <InfoTip label="About memory">
          The items this trigger filed, by the source’s id, open ones and ones closed this week, and the note the agent
          left itself for the next check. A closed one is filed again only when the agent says why.
        </InfoTip>
      </span>
      {memory ? (
        <p className="m-0 flex flex-wrap items-center gap-x-5 gap-y-1 text-secondary">
          <span>{memory.open === 1 ? '1 open item' : `${memory.open} open items`}</span>
          <span>{`${memory.closed} closed this week`}</span>
          {memory.checked_up_to ? <span>{`Checked up to ${checkedUpTo(memory.checked_up_to)}`}</span> : null}
          {memory.state !== null && onClear ? (
            <Button
              variant="ghost"
              size="sm"
              busy={busy}
              busyLabel="Clearing…"
              onClick={() => void clear()}
              aria-label="Clear what it remembers"
              className="h-auto px-0 text-run-ink"
            >
              Clear
            </Button>
          ) : null}
        </p>
      ) : (
        <p className="m-0 text-secondary text-muted">Nothing yet. It remembers once the first check has run.</p>
      )}
      {error ? (
        <p role="alert" className="m-0 text-secondary text-rework-ink">
          {error}
        </p>
      ) : null}
    </div>
  );
}

/** A dollar amount typed in place; text that is not a positive number is not saved and resets on blur. */
export function Usd({ label, value, onChange }: { label: string; value: number; onChange: (n: number) => void }) {
  const [text, setText] = useState(value.toFixed(2));
  return (
    <Input
      type="number"
      inputMode="decimal"
      min={0.01}
      step={0.01}
      aria-label={label}
      value={text}
      onChange={(e) => {
        setText(e.target.value);
        const n = Number(e.target.value);
        if (Number.isFinite(n) && n > 0) onChange(n);
      }}
      onBlur={() => setText(value.toFixed(2))}
      className="h-8 w-20 text-center"
    />
  );
}
