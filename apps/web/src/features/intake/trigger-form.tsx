// The trigger's four blocks as the picture draws them (MASTER.md, "Sentence builder"): When, Only if,
// Create and Limits. Each control writes one key of the trigger through `set`; nothing here saves. A
// control for a feature not built yet renders through NotAvailable (ADR 0016, decision 4).
import { ChevronDown, Plus } from 'lucide-react';
import { type ReactNode, useId, useState } from 'react';
import type { Trigger, TriggerFilter } from '../../shared/definition/model';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { Field, Input, NativeSelect, Textarea } from '../../ui/field';
import { NotAvailable } from '../../ui/not-available';
import type { BoardChoices, TriggerBlock } from './trigger-yaml';

/** The problems of each block, by the block the problem's line sits in. */
export type BlockProblems = Partial<Record<TriggerBlock, string[]>>;

/** The event keys the page offers; a file may hold another one, which stays selectable as it is. */
const events = [
  { key: 'github.check_run.failed', label: 'a check run fails' },
  { key: 'github.issues.labeled', label: 'an issue is labeled' },
];

/** The fields a check-run event carries, which a template may insert. */
const checkRunFields = ['check.name', 'check.logs_url'];

const sourceOf = (on: string) => {
  if (on.startsWith('github.')) return 'GitHub';
  if (on.startsWith('webhook.')) return 'Webhook';
  return 'Schedule';
};

const listText = (list: string[] | undefined) => (list ?? []).join(', ');
const listOf = (text: string) =>
  text
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);

/** A block of the form: the picture's card with its header bar and its problems under the controls. */
function Block({
  id,
  title,
  hint,
  problems = [],
  children,
}: {
  id: string;
  title: string;
  hint?: string | undefined;
  problems?: string[] | undefined;
  children: ReactNode;
}) {
  return (
    <section aria-labelledby={id} className="overflow-hidden rounded-card border border-line">
      <header className="flex flex-wrap items-center gap-3 border-b border-line bg-panel px-4 py-3">
        <h2 id={id} className="m-0 text-body font-semibold">
          {title}
        </h2>
        {hint ? <p className="m-0 ml-auto text-secondary text-muted">{hint}</p> : null}
      </header>
      <div className="flex flex-col gap-3 p-4">
        {children}
        <Problems messages={problems} />
      </div>
    </section>
  );
}

/** Validation problems under the field they belong to: the server's words, never a guess of ours. */
export function Problems({ messages }: { messages: string[] }) {
  return messages.map((message) => (
    <p key={message} role="alert" className="m-0 text-secondary text-rework-ink">
      {message}
    </p>
  ));
}

/** A whole number of at least one, typed in place. Text that is not one is not saved; it resets on blur. */
function Count({
  label,
  value,
  onChange,
  disabled,
}: {
  label: string;
  value: number;
  onChange: (n: number) => void;
  disabled?: boolean;
}) {
  const [text, setText] = useState(String(value));
  return (
    <Input
      type="number"
      inputMode="numeric"
      min={1}
      step={1}
      aria-label={label}
      disabled={disabled}
      value={text}
      onChange={(e) => {
        setText(e.target.value);
        const n = Number(e.target.value);
        if (Number.isInteger(n) && n >= 1) onChange(n);
      }}
      onBlur={() => setText(String(value))}
      className="h-8 w-16 text-center"
    />
  );
}

/** A check box with its words beside it. */
function Option({
  checked,
  onChange,
  children,
}: {
  checked: boolean;
  onChange: (on: boolean) => void;
  children: ReactNode;
}) {
  return (
    <label className="flex items-start gap-3 text-body">
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="mt-1 size-4 shrink-0 accent-run"
      />
      <span className="flex min-w-0 flex-wrap items-center gap-2">{children}</span>
    </label>
  );
}

export interface TriggerFormProps {
  value: Trigger;
  set: <K extends keyof Trigger>(key: K, v: Trigger[K]) => void;
  board: BoardChoices;
  factoryName: string;
  problems: BlockProblems;
}

/** The four blocks of the trigger, in the picture's order. */
export function TriggerForm({ value, set, board, factoryName, problems }: TriggerFormProps) {
  const ids = { when: useId(), only: useId(), create: useId(), limits: useId() };
  const [more, setMore] = useState(false);
  const moreId = useId();
  const filter: TriggerFilter = value.filter ?? {};
  const setFilter = (patch: Partial<TriggerFilter>) => {
    const next = { ...filter, ...patch };
    const kept = Object.fromEntries(
      Object.entries(next).filter(
        ([, v]) => v !== undefined && v !== '' && v !== false && !(Array.isArray(v) && !v.length),
      ),
    ) as TriggerFilter;
    set('filter', Object.keys(kept).length ? kept : undefined);
  };
  const only = value.only ?? {};
  const model = only.modelAgrees;
  const create = value.create;
  const limits = value.limits ?? {};
  const fields = value.on === 'github.check_run.failed' ? checkRunFields : [];
  const eventChoices = events.some((e) => e.key === value.on)
    ? events
    : [...events, { key: value.on, label: value.on }];
  const laneValue = create.expedite ? 'expedite' : (create.lane ?? '');
  const types =
    create.type && !board.workTypes.includes(create.type) ? [...board.workTypes, create.type] : board.workTypes;
  const stations =
    create.station && !board.stations.includes(create.station) ? [...board.stations, create.station] : board.stations;
  // The lanes are the keys of the chosen station's route (factory.schema.json, station.route).
  const lanes = board.lanes[create.station] ?? [];

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Block
        id={ids.when}
        title="When"
        hint="Any one of these events"
        problems={[...(problems.on ?? []), ...(problems.filter ?? [])]}
      >
        <div className="flex flex-wrap items-center gap-2">
          <span className="inline-flex h-8 items-center rounded-control border border-rule bg-card px-3 text-body font-control">
            {sourceOf(value.on)}
          </span>
          <NativeSelect
            aria-label="Event"
            value={value.on}
            onChange={(e) => set('on', e.target.value)}
            className="h-8 rounded-control border-rule bg-card px-2 text-body"
          >
            {eventChoices.map((e) => (
              <option key={e.key} value={e.key}>
                {e.label}
              </option>
            ))}
          </NativeSelect>
          <span className="text-body text-muted">on</span>
          <Input
            aria-label="Branch"
            placeholder="Any branch"
            value={filter.branch ?? ''}
            onChange={(e) => setFilter({ branch: e.target.value || undefined })}
            className="h-8 w-28 rounded-control text-body"
          />
          <span className="text-body text-muted">in</span>
          <Input
            aria-label="Repository"
            placeholder="Any repository"
            value={filter.repo ?? ''}
            onChange={(e) => setFilter({ repo: e.target.value || undefined })}
            className="h-8 w-44 rounded-control text-body"
          />
          {value.on === 'github.issues.labeled' ? (
            <Input
              aria-label="Label"
              placeholder="Any label"
              value={filter.label ?? ''}
              onChange={(e) => setFilter({ label: e.target.value || undefined })}
              className="h-8 w-32 rounded-control text-body"
            />
          ) : null}
        </div>
        {fields.length ? (
          <p className="m-0 flex flex-wrap items-center gap-2 text-secondary text-muted">
            Fields:
            {fields.map((f) => (
              <code key={f} className="rounded-chip bg-you-tint px-2 py-1 font-mono text-meta text-you-ink">
                {f}
              </code>
            ))}
          </p>
        ) : null}
        <div className="flex flex-wrap gap-x-5 gap-y-2">
          <Button
            variant="ghost"
            size="sm"
            aria-expanded={more}
            aria-controls={moreId}
            onClick={() => setMore((open) => !open)}
            className="px-0 text-run-ink"
          >
            More filters: paths, authors, skip bots
            <ChevronDown size={14} strokeWidth={1.5} aria-hidden="true" className={cn(more && 'rotate-180')} />
          </Button>
          <NotAvailable label="Add another event">
            <Button variant="ghost" size="sm" className="px-0 text-run-ink">
              <Plus size={14} strokeWidth={1.5} aria-hidden="true" />
              Add another event
            </Button>
          </NotAvailable>
        </div>
        {more ? (
          <div id={moreId} className="grid gap-3 rounded-cell border border-line bg-panel p-3 sm:grid-cols-2">
            <Field label="Paths (globs, comma separated)">
              <Input
                value={listText(filter.paths)}
                placeholder="src/**, apps/web/**"
                onChange={(e) => setFilter({ paths: listOf(e.target.value) })}
                className="h-8 rounded-control text-body"
              />
            </Field>
            <Field label="Authors (comma separated)">
              <Input
                value={listText(filter.authors)}
                placeholder="dependabot, octocat"
                onChange={(e) => setFilter({ authors: listOf(e.target.value) })}
                className="h-8 rounded-control text-body"
              />
            </Field>
            <Option checked={filter.skipBots === true} onChange={(on) => setFilter({ skipBots: on || undefined })}>
              Skip bots
            </Option>
          </div>
        ) : null}
      </Block>

      <Block id={ids.only} title="Only if" hint="Checked before any item is created" problems={problems.only}>
        <Option
          checked={only.failuresInARow !== undefined}
          onChange={(on) => set('only', { ...only, failuresInARow: on ? 2 : undefined })}
        >
          The same check failed
          <Count
            label="Failures in a row"
            value={only.failuresInARow ?? 2}
            disabled={only.failuresInARow === undefined}
            onChange={(n) => set('only', { ...only, failuresInARow: n })}
          />
          times in a row
        </Option>
        <Option
          checked={only.dedupe !== undefined}
          onChange={(on) => set('only', { ...only, dedupe: on ? 'check.name' : undefined })}
        >
          No open item already covers this check
        </Option>
        <Option
          checked={only.modelAgrees !== undefined}
          onChange={(on) => set('only', { ...only, modelAgrees: on ? { question: '' } : undefined })}
        >
          A model agrees it is worth an item
          <span className="text-muted">, using the question you write, about $0.01 each time</span>
        </Option>
        {model ? (
          <div className="pl-7">
            <Field
              label="Question for the model"
              help="The model answers yes or no about the event. It reads the event as untrusted data."
              error={model.question.trim() === '' ? 'Write the question the model answers. Save needs it.' : undefined}
            >
              <Input
                value={model.question}
                maxLength={500}
                onChange={(e) => set('only', { ...only, modelAgrees: { ...model, question: e.target.value } })}
              />
            </Field>
          </div>
        ) : null}
      </Block>

      <Block id={ids.create} title="Create" hint="Goes onto the line like any other item" problems={problems.create}>
        <div className="flex flex-wrap items-center gap-2 text-body">
          <span>A</span>
          <NativeSelect
            aria-label="Item type"
            value={create.type}
            onChange={(e) => set('create', { ...create, type: e.target.value })}
            className="h-8 rounded-control border-rule bg-card px-2 text-body"
          >
            {types.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </NativeSelect>
          <span>item that starts at</span>
          <NativeSelect
            aria-label="Starts at station"
            value={create.station}
            onChange={(e) => set('create', { ...create, station: e.target.value })}
            className="h-8 rounded-control border-rule bg-card px-2 text-body"
          >
            {stations.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </NativeSelect>
          <span>in the</span>
          <NativeSelect
            aria-label="Lane"
            value={laneValue}
            onChange={(e) => {
              const v = e.target.value;
              const { lane: _lane, expedite: _expedite, ...rest } = create;
              set('create', v === 'expedite' ? { ...rest, expedite: true } : v === '' ? rest : { ...rest, lane: v });
            }}
            className={cn(
              'h-8 rounded-control border-rule px-2 text-body',
              create.expedite ? 'bg-amber-tint text-amber-ink' : 'bg-card',
            )}
          >
            <option value="">Standard lane</option>
            <option value="expedite">Expedite lane</option>
            {lanes.map((lane) => (
              <option key={lane} value={lane}>
                {lane} lane
              </option>
            ))}
            {create.lane && !lanes.includes(create.lane) ? (
              <option value={create.lane}>{create.lane} lane</option>
            ) : null}
          </NativeSelect>
        </div>
        <TemplateField
          label="Title"
          value={create.title ?? ''}
          fields={fields}
          onChange={(v) => set('create', { ...create, title: v || undefined })}
          multiline={false}
        />
        <TemplateField
          label="Brief for the agent"
          value={create.brief ?? ''}
          fields={fields}
          onChange={(v) => set('create', { ...create, brief: v || undefined })}
          multiline
          help="Text from the event reaches the agent marked as untrusted data, never as instructions."
        />
      </Block>

      <Block id={ids.limits} title="Limits" problems={problems.limits}>
        <Option
          checked={limits.perDay !== undefined}
          onChange={(on) => set('limits', { ...limits, perDay: on ? 3 : undefined })}
        >
          At most
          <Count
            label="Items a day"
            value={limits.perDay ?? 3}
            disabled={limits.perDay === undefined}
            onChange={(n) => set('limits', { ...limits, perDay: n })}
          />
          items a day
        </Option>
        <Option
          checked={limits.pauseOnBudget === true}
          onChange={(on) => set('limits', { ...limits, pauseOnBudget: on || undefined })}
        >
          Pause when {factoryName}&apos;s weekly budget is spent
        </Option>
      </Block>
    </div>
  );
}

/** A template with the event's fields as chips to insert; the placeholders are written as {{field}}. */
function TemplateField({
  label,
  value,
  fields,
  onChange,
  multiline,
  help,
}: {
  label: string;
  value: string;
  fields: string[];
  onChange: (v: string) => void;
  multiline: boolean;
  help?: string;
}) {
  const id = useId();
  const insert = (field: string) => onChange(`${value}{{${field}}}`);
  const common = 'rounded-cell border-rule bg-card px-3 py-2 text-body';
  return (
    <div className="flex flex-col gap-2">
      <label htmlFor={id} className="text-secondary font-semibold">
        {label}
      </label>
      {multiline ? (
        <Textarea id={id} rows={4} value={value} onChange={(e) => onChange(e.target.value)} className={common} />
      ) : (
        <Input id={id} value={value} onChange={(e) => onChange(e.target.value)} className={cn('h-9', common)} />
      )}
      {fields.length ? (
        <div className="flex flex-wrap items-center gap-2">
          {fields.map((f) => (
            <Button
              key={f}
              variant="ghost"
              size="sm"
              onClick={() => insert(f)}
              className="h-6 px-2 font-mono text-meta"
            >
              {`{{${f}}}`}
            </Button>
          ))}
        </div>
      ) : null}
      {help ? <p className="m-0 text-meta text-muted">{help}</p> : null}
    </div>
  );
}
