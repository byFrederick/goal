// The Knowledge section's cards (ADR 0038 decision 12): the factory's sources, factory memory and what
// agents see. Each takes its values and reports edits, as the Environments cards do; the section wires
// them to the draft.
import { Link } from '@tanstack/react-router';
import { useState } from 'react';
import type { KnowledgeSource } from '../../../api/types';
import { useOrg } from '../../../shared/org';
import { useArrivals } from '../../../ui/arrivals';
import { Button } from '../../../ui/button';
import { cn } from '../../../ui/cn';
import { Field, Textarea } from '../../../ui/field';
import { Select } from '../../../ui/select';
import { sizeOf } from '../../knowledge/facts';
import { Card, RemoveButton, rowPad } from '../environments/parts';
import {
  addEntry,
  editEntry,
  type MemoryEntry,
  maxMemoryBytes,
  maxMemoryEntries,
  maxMemoryText,
  memoryProblem,
  removeEntry,
} from './memory-file';
import { readersOf, type SourceEntry } from './source-entries';

const rowClass = cn(
  'flex flex-col gap-2 border-t border-line py-3 first:border-t-0 sm:flex-row sm:items-start',
  rowPad,
);
const arrivalClass = 'motion-safe:animate-item-in motion-reduce:animate-fade-in';

export function SourcesCard({
  entries,
  known,
  readOnly,
  onAdd,
  onRemove,
}: {
  /** The factory's entries, in its order. */
  entries: readonly SourceEntry[];
  /** The organization's sources, to name sizes and to pick from. */
  known: readonly KnowledgeSource[];
  readOnly: boolean;
  onAdd: (name: string) => void;
  onRemove: (name: string) => void;
}) {
  const org = useOrg();
  const arrived = useArrivals(entries.map((e) => e.name));
  const [pick, setPick] = useState('');
  const listed = new Set(entries.map((e) => e.name));
  const choices = known.filter((s) => !listed.has(s.name));
  return (
    <Card title="Sources" hint="Folders of files agents read. Each run gets the ones that apply to it.">
      {entries.length === 0 ? (
        <p className={cn('m-0 py-4 text-sm text-muted', rowPad)}>
          No sources yet. Agents get no files from the organization.
        </p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {entries.map((e) => {
            const source = known.find((s) => s.name === e.name);
            const size = source ? sizeOf(source) : undefined;
            const line = e.readWhen ?? source?.read_when;
            return (
              <li key={e.name} className={cn(rowClass, arrived(e.name) && arrivalClass)}>
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <Link
                    to="/o/$org/knowledge/$source"
                    params={{ org: org.slug, source: e.name }}
                    className="w-fit font-mono text-sm text-ink"
                  >
                    {e.name}
                  </Link>
                  <span className="text-sm text-muted">{readersOf(e)}</span>
                  {line ? <span className="text-sm text-ink">{line}</span> : null}
                  {source === undefined ? (
                    <span className="text-sm text-rework-ink">Not in this organization’s sources.</span>
                  ) : null}
                </div>
                {size ? (
                  <span className="text-sm text-muted sm:text-right">
                    {size.files} · {size.bytes}
                  </span>
                ) : null}
                {readOnly ? null : <RemoveButton label={`Remove ${e.name}`} onClick={() => onRemove(e.name)} />}
              </li>
            );
          })}
        </ul>
      )}
      {readOnly ? null : (
        <div className={cn('flex flex-wrap items-end gap-2 border-t border-line py-4', rowPad)}>
          {choices.length === 0 ? (
            <span className="text-sm text-muted">
              Every source is listed.{' '}
              <Link to="/o/$org/knowledge/new" params={{ org: org.slug }}>
                Add a source
              </Link>
            </span>
          ) : (
            <>
              <Field label="Add a source" className="min-w-0 flex-1 sm:max-w-xs">
                <Select
                  value={pick}
                  placeholder="Pick a source"
                  options={choices.map((s) => ({ value: s.name, label: s.name }))}
                  onValueChange={setPick}
                />
              </Field>
              <Button
                variant="secondary"
                disabled={pick === ''}
                onClick={() => {
                  onAdd(pick);
                  setPick('');
                }}
              >
                Add source
              </Button>
            </>
          )}
        </div>
      )}
    </Card>
  );
}

export function MemoryCard({
  entries,
  readOnly,
  bytes,
  onChange,
}: {
  entries: readonly MemoryEntry[];
  readOnly: boolean;
  /** The file's size now. */
  bytes: number;
  onChange: (next: MemoryEntry[]) => void;
}) {
  const arrived = useArrivals(entries.map((e) => `${e.added}|${e.text}`));
  const [draft, setDraft] = useState('');
  const problem = draft.trim() === '' ? undefined : memoryProblem(entries, -1, draft);
  return (
    <Card
      title="Factory memory"
      hint={`Short facts every run starts with. ${entries.length} of ${maxMemoryEntries}, ${Math.ceil(bytes / 1024)} of ${maxMemoryBytes / 1024} KB.`}
    >
      {entries.length === 0 ? (
        <p className={cn('m-0 py-4 text-sm text-muted', rowPad)}>No facts yet.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {entries.map((e, i) => (
            <MemoryRow
              key={`${e.added}|${e.text}`}
              entries={entries}
              index={i}
              readOnly={readOnly}
              arrived={arrived(`${e.added}|${e.text}`)}
              onChange={onChange}
            />
          ))}
        </ul>
      )}
      {readOnly ? null : (
        <form
          className={cn('flex flex-col gap-2 border-t border-line py-4', rowPad)}
          onSubmit={(ev) => {
            ev.preventDefault();
            if (draft.trim() === '' || problem) return;
            onChange(addEntry(entries, draft));
            setDraft('');
          }}
        >
          <Field label="Add a fact" error={problem} help={`${draft.trim().length} of ${maxMemoryText} characters`}>
            <Textarea
              value={draft}
              rows={2}
              onChange={(ev) => setDraft(ev.target.value)}
              className="field-sizing-content min-h-[4.5rem]"
            />
          </Field>
          <div>
            <Button type="submit" variant="secondary" disabled={draft.trim() === '' || problem !== undefined}>
              Add fact
            </Button>
          </div>
        </form>
      )}
    </Card>
  );
}

function MemoryRow({
  entries,
  index,
  readOnly,
  arrived,
  onChange,
}: {
  entries: readonly MemoryEntry[];
  index: number;
  readOnly: boolean;
  arrived: boolean;
  onChange: (next: MemoryEntry[]) => void;
}) {
  const entry = entries[index] as MemoryEntry;
  const [text, setText] = useState(entry.text);
  const problem = text.trim() === entry.text ? undefined : memoryProblem(entries, index, text);
  const commit = () => {
    if (text.trim() === entry.text) return;
    if (problem) return;
    onChange(editEntry(entries, index, text));
  };
  return (
    <li className={cn(rowClass, arrived && arrivalClass)}>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <Field label={<span className="sr-only">Fact {index + 1}</span>} error={problem}>
          <Textarea
            value={text}
            rows={1}
            readOnly={readOnly}
            onChange={(ev) => setText(ev.target.value)}
            onBlur={commit}
            className="field-sizing-content"
          />
        </Field>
        <span className="text-sm text-muted">
          {entry.added}
          {entry.from.length > 0 ? ` · from ${entry.from.join(', ')}` : ''}
        </span>
      </div>
      {readOnly ? null : (
        <RemoveButton label={`Remove fact ${index + 1}`} onClick={() => onChange(removeEntry(entries, index))} />
      )}
    </li>
  );
}

export function PreviewCard({
  agents,
  agent,
  onAgent,
  section,
  loading,
  failed,
}: {
  agents: readonly string[];
  agent: string;
  onAgent: (name: string) => void;
  /** The section as Markdown; "" when there is none. */
  section: string | undefined;
  loading: boolean;
  failed: boolean;
}) {
  return (
    <Card title="What agents see" hint="The Knowledge section of the prompt, as one agent gets it.">
      <div className={cn('flex flex-col gap-3 py-4', rowPad)}>
        {agents.length === 0 ? (
          <p className="m-0 text-sm text-muted">No agents yet.</p>
        ) : (
          <>
            <Field label="Agent" className="sm:max-w-xs">
              <Select value={agent} options={agents.map((a) => ({ value: a, label: a }))} onValueChange={onAgent} />
            </Field>
            {failed ? (
              <p className="m-0 text-sm text-rework-ink">The preview needs a draft that saves and passes its checks.</p>
            ) : loading ? (
              <p className="m-0 text-sm text-muted" aria-live="polite">
                Loading the preview…
              </p>
            ) : section === undefined || section.trim() === '' ? (
              <p className="m-0 text-sm text-muted">This agent gets no Knowledge section.</p>
            ) : (
              <pre className="m-0 max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-control border border-line bg-shell p-3 font-mono text-sm leading-normal">
                {section}
              </pre>
            )}
          </>
        )}
      </div>
    </Card>
  );
}
