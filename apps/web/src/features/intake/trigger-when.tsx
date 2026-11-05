// The When block (ADR 0040 decision 20): the sender or one webhook, any event, the filters that event
// supplies and payload matches. Each control writes one key of the trigger through `onChange`; nothing
// here saves. A filter the event cannot supply is not offered; the draft's own check stays the last word.
import { ChevronDown, Plus, Trash2 } from 'lucide-react';
import { type ReactNode, useId, useRef, useState } from 'react';
import type { Trigger, TriggerFilter, TriggerValues } from '../../shared/definition/model';
import { Button } from '../../ui/button';
import { Field, Input, NativeSelect } from '../../ui/field';
import { NotAvailable } from '../../ui/not-available';
import { type BlockProblems, Block, ChipList, fromList, listOf, Option } from './form-parts';
import { type FilterKey, githubEventOf, githubEvents } from './github-events';

type Source = 'schedule' | 'github' | 'webhook';

const sourceOf = (on: string): Source => (on === 'schedule' ? 'schedule' : on.startsWith('github.') ? 'github' : 'webhook');

/** The schedules the select offers; a cron outside this list is "Custom" and shows its own field. */
const schedules = [
  { cron: '*/5 * * * *', label: '5 minutes' },
  { cron: '*/15 * * * *', label: '15 minutes' },
  { cron: '0 * * * *', label: 'hour' },
  { cron: '0 8 * * *', label: 'day at 08:00' },
  { cron: '0 8 * * 1-5', label: 'weekday at 08:00' },
];
const custom = 'custom';

/** "Every 5 minutes": when a schedule fires, in words; a cron outside the list is shown as written. */
export const cronWords = (cron: string) => {
  const known = schedules.find((s) => s.cron === cron);
  return known ? `Every ${known.label}` : `Cron ${cron}`;
};

const clean = (filter: TriggerFilter): TriggerFilter | undefined => {
  const kept = Object.fromEntries(
    Object.entries(filter).filter(
      ([, v]) =>
        v !== undefined &&
        v !== '' &&
        v !== false &&
        !(Array.isArray(v) && !v.length) &&
        !(typeof v === 'object' && !Array.isArray(v) && Object.keys(v).length === 0),
    ),
  ) as TriggerFilter;
  return Object.keys(kept).length ? kept : undefined;
};

export interface WhenProps {
  value: Trigger;
  onChange: (next: Trigger) => void;
  problems?: BlockProblems;
  /** The ⓘ of the block; the page names what the event supplies. */
  id: string;
}

export function TriggerWhen({ value, onChange, problems = {}, id }: WhenProps) {
  const [more, setMore] = useState(false);
  const moreId = useId();
  const source = sourceOf(value.on);
  const event = githubEventOf(value.on);
  const filter: TriggerFilter = value.filter ?? {};
  const setFilter = (patch: Partial<TriggerFilter>) => onChange({ ...value, filter: clean({ ...filter, ...patch }) });
  const offers = (key: FilterKey) => (source === 'github' ? (event?.filters.includes(key) ?? true) : true);
  const info =
    source === 'github' && event
      ? `${event.event} supplies ${[...new Set(event.filters.map((f) => (f === 'authors' ? 'authors' : f === 'skipBots' ? 'skip bots' : f)))].join(', ')}. A filter the event can't supply is refused when you save.`
      : 'Any one of these events starts the trigger. A filter the event can’t supply is refused when you save.';

  const pickSource = (next: Source) => {
    if (next === source) return;
    const { cron: _c, timeZone: _t, check: _k, ...rest } = value;
    if (next === 'schedule') onChange({ ...rest, on: 'schedule', cron: schedules[0]?.cron ?? '*/5 * * * *', filter: undefined });
    else if (next === 'github') onChange({ ...rest, on: `github.${githubEvents[0]?.event ?? 'check_run'}`, filter: undefined });
    else onChange({ ...rest, on: '', filter: undefined });
  };

  const [hook = '', ...rest] = value.on.split('.');
  const sourceControl = (
    <NativeSelect
      aria-label="Source"
      value={source}
      onChange={(e) => pickSource(e.target.value as Source)}
      className="h-8 rounded-control border-rule bg-card px-2 text-body font-control"
    >
      <option value="schedule">Schedule</option>
      <option value="github">GitHub</option>
      <option value="webhook">Webhook</option>
    </NativeSelect>
  );

  return (
    <Block
      id={id}
      title="When"
      info={info}
      problems={[...(problems.on ?? []), ...(problems.filter ?? [])]}
    >
      {source === 'schedule' ? (
        <ScheduleRow value={value} onChange={onChange} sourceControl={sourceControl} />
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2">
            {sourceControl}
            {source === 'github' ? (
              <NativeSelect
                aria-label="Event"
                value={value.on}
                onChange={(e) => onChange({ ...value, on: e.target.value, filter: undefined })}
                className="h-8 rounded-control border-rule bg-card px-2 text-body"
              >
                {githubEvents.map((e) => (
                  <option key={e.event} value={`github.${e.event}`}>
                    {e.label}
                  </option>
                ))}
                {event ? null : <option value={value.on}>{value.on}</option>}
              </NativeSelect>
            ) : (
              <>
                <Input
                  aria-label="Webhook"
                  placeholder="Webhook name"
                  value={hook}
                  onChange={(e) => onChange({ ...value, on: [e.target.value.trim(), ...rest].filter(Boolean).join('.') })}
                  className="h-8 w-44 rounded-control text-body"
                />
                <Input
                  aria-label="Webhook event"
                  placeholder="Any event"
                  value={rest.join('.')}
                  onChange={(e) =>
                    onChange({ ...value, on: [hook, e.target.value.trim()].filter(Boolean).join('.') })
                  }
                  className="h-8 w-36 rounded-control text-body"
                />
              </>
            )}
            {offers('action') ? (
              <ChipList
                label="Action"
                prefix="action"
                values={listOf(filter.action)}
                onChange={(l) => setFilter({ action: fromList(l) })}
                placeholder="Any action"
              />
            ) : null}
            {offers('conclusion') ? (
              <ChipList
                label="Conclusion"
                prefix="conclusion"
                values={listOf(filter.conclusion)}
                onChange={(l) => setFilter({ conclusion: fromList(l) })}
                placeholder="Any conclusion"
              />
            ) : null}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {offers('branch') ? (
              <>
                <span className="text-body text-muted">on</span>
                <Input
                  aria-label="Branch"
                  placeholder="Any branch"
                  value={filter.branch ?? ''}
                  onChange={(e) => setFilter({ branch: e.target.value || undefined })}
                  className="h-8 w-32 rounded-control text-body"
                />
              </>
            ) : null}
            {offers('repo') ? (
              <>
                <span className="text-body text-muted">in</span>
                <ChipList
                  label="Repository"
                  values={listOf(filter.repo)}
                  onChange={(l) => setFilter({ repo: fromList(l) })}
                  placeholder="Any repository"
                />
              </>
            ) : null}
            {offers('label') ? (
              <Input
                aria-label="Label"
                placeholder="Any label"
                value={filter.label ?? ''}
                onChange={(e) => setFilter({ label: e.target.value || undefined })}
                className="h-8 w-32 rounded-control text-body"
              />
            ) : null}
          </div>
          <div className="flex flex-wrap gap-x-5 gap-y-2">
            <Button
              variant="ghost"
              size="sm"
              aria-expanded={more}
              aria-controls={moreId}
              onClick={() => setMore((open) => !open)}
              data-state={more ? 'open' : 'closed'}
              className="motion-disclosure-trigger px-0 text-run-ink"
            >
              More filters
              <ChevronDown size={14} strokeWidth={1.5} aria-hidden="true" className="motion-chevron" />
            </Button>
            <NotAvailable label="Add another event">
              <Button variant="ghost" size="sm" className="px-0 text-run-ink">
                <Plus size={14} strokeWidth={1.5} aria-hidden="true" />
                Add another event
              </Button>
            </NotAvailable>
          </div>
          <div
            id={moreId}
            data-state={more ? 'open' : 'closed'}
            aria-hidden={more ? undefined : true}
            inert={!more}
            className="motion-disclosure"
          >
            <div className="grid gap-3 rounded-cell border border-line bg-panel p-3 sm:grid-cols-2">
              {offers('paths') ? (
                <Field label="Paths (globs)">
                  <ChipList
                    label="Paths"
                    values={filter.paths ?? []}
                    onChange={(l) => setFilter({ paths: l })}
                    placeholder="src/**"
                  />
                </Field>
              ) : null}
              <Field label="Authors">
                <ChipList
                  label="Authors"
                  values={filter.authors ?? []}
                  onChange={(l) => setFilter({ authors: l })}
                  placeholder="octocat"
                />
              </Field>
              <Option checked={filter.skipBots === true} onChange={(on) => setFilter({ skipBots: on || undefined })}>
                Skip bots
              </Option>
              <div className="sm:col-span-2">
                <Matches match={filter.match} onChange={(match) => setFilter({ match })} />
              </div>
            </div>
          </div>
        </>
      )}
    </Block>
  );
}

function ScheduleRow({
  value,
  onChange,
  sourceControl,
}: {
  value: Trigger;
  onChange: (next: Trigger) => void;
  sourceControl: ReactNode;
}) {
  const cron = value.cron ?? '';
  const preset = schedules.find((s) => s.cron === cron);
  const [typing, setTyping] = useState(preset === undefined && cron !== '');
  const isCustom = typing || preset === undefined;
  return (
    <div className="flex flex-wrap items-center gap-2">
      {sourceControl}
      <span className="text-body text-muted">every</span>
      <NativeSelect
        aria-label="Every"
        value={isCustom ? custom : cron}
        onChange={(e) => {
          if (e.target.value === custom) setTyping(true);
          else {
            setTyping(false);
            onChange({ ...value, cron: e.target.value });
          }
        }}
        className="h-8 rounded-control border-rule bg-card px-2 text-body"
      >
        {schedules.map((s) => (
          <option key={s.cron} value={s.cron}>
            {s.label}
          </option>
        ))}
        <option value={custom}>Custom cron</option>
      </NativeSelect>
      {isCustom ? (
        <Input
          aria-label="Cron"
          placeholder="*/5 * * * *"
          value={cron}
          onChange={(e) => onChange({ ...value, cron: e.target.value })}
          className="h-8 w-40 rounded-control font-mono text-body"
        />
      ) : null}
      <Input
        aria-label="Time zone"
        placeholder="UTC"
        value={value.timeZone ?? ''}
        onChange={(e) => onChange({ ...value, timeZone: e.target.value || undefined })}
        className="h-8 w-36 rounded-control text-body"
      />
    </div>
  );
}

interface Row {
  id: number;
  path: string;
  values: string[];
}

/** Payload matches: a dot path into the delivery's body and the values it may have; every row must match. */
function Matches({
  match,
  onChange,
}: {
  match: Record<string, TriggerValues> | undefined;
  onChange: (next: Record<string, TriggerValues> | undefined) => void;
}) {
  const [rows, setRows] = useState<Row[]>(() =>
    Object.entries(match ?? {}).map(([path, v], i) => ({ id: i, path, values: listOf(v) })),
  );
  const nextId = useRef(rows.length);
  const commit = (list: Row[]) => {
    setRows(list);
    const entries = list.filter((r) => r.path.trim() && r.values.length);
    onChange(
      entries.length
        ? Object.fromEntries(entries.map((r) => [r.path.trim(), fromList(r.values) as TriggerValues]))
        : undefined,
    );
  };
  const edit = (id: number, patch: Partial<Row>) => commit(rows.map((r) => (r.id === id ? { ...r, ...patch } : r)));
  return (
    <div role="group" aria-label="Payload matches" className="flex flex-col gap-2">
      <span className="text-secondary font-semibold">Payload matches</span>
      {rows.map((row) => (
        <div key={row.id} className="flex flex-wrap items-center gap-2">
          <Input
            aria-label="Payload path"
            placeholder="check_run.app.slug"
            value={row.path}
            onChange={(e) => edit(row.id, { path: e.target.value })}
            className="h-8 w-56 rounded-control font-mono text-body"
          />
          <span className="text-body text-muted">is</span>
          <ChipList
            label="Payload values"
            values={row.values}
            onChange={(values) => edit(row.id, { values })}
            placeholder="Value"
          />
          <Button
            variant="ghost"
            size="icon"
            aria-label={`Remove the match on ${row.path || 'this path'}`}
            onClick={() => commit(rows.filter((r) => r.id !== row.id))}
          >
            <Trash2 size={14} strokeWidth={1.5} aria-hidden="true" />
          </Button>
        </div>
      ))}
      <div>
        <Button
          variant="ghost"
          size="sm"
          className="px-0 text-run-ink"
          onClick={() => setRows([...rows, { id: nextId.current++, path: '', values: [] }])}
        >
          <Plus size={14} strokeWidth={1.5} aria-hidden="true" />
          Add a payload match
        </Button>
      </div>
    </div>
  );
}
