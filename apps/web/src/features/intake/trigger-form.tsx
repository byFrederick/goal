// The trigger's blocks as the pictures draw them (MASTER.md, "Sentence builder"): When, Only if, Create and
// Limits for an event trigger; When, Check, Create and Limits for a check. Each control writes through
// `onChange`; nothing here saves or fetches, so the same form serves a saved trigger's page and a new one.
import { useId, useState } from 'react';
import type { TriggerCheckMemory } from '../../api/types';
import type { Trigger } from '../../shared/definition/model';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { Input, NativeSelect, Textarea } from '../../ui/field';
import { Switch } from '../../ui/switch';
import { InfoTip } from '../jonah/info-tip';
import { Block, type BlockProblems, Count, Option } from './form-parts';
import { templateFieldsOf } from './github-events';
import { hasVariable, TemplateText } from './template-text';
import { CheckSection, Usd } from './trigger-check-section';
import { TriggerWhen } from './trigger-when';
import type { BoardChoices } from './trigger-yaml';

export type { BlockProblems } from './form-parts';
export { Problems } from './form-parts';

export interface TriggerFormProps {
  value: Trigger;
  onChange: (next: Trigger) => void;
  board: BoardChoices;
  factoryName: string;
  problems: BlockProblems;
  /** What a saved check trigger remembers, and how to clear it; both absent for a trigger not saved yet. */
  memory?: TriggerCheckMemory | undefined;
  onClear?: (() => Promise<void>) | undefined;
}

/** The blocks of the trigger, in the picture's order. */
export function TriggerForm({ value, onChange, board, factoryName, problems, memory, onClear }: TriggerFormProps) {
  const ids = { when: useId(), only: useId(), check: useId(), create: useId(), limits: useId() };
  const isCheck = value.check !== undefined;
  const set = <K extends keyof Trigger>(key: K, v: Trigger[K]) => onChange({ ...value, [key]: v });
  const only = value.only ?? {};
  const create = value.create;
  const limits = value.limits ?? {};
  const setLimits = (patch: Partial<NonNullable<Trigger['limits']>>) => set('limits', { ...limits, ...patch });
  const fields = templateFieldsOf(value.on);
  const laneValue = create.expedite ? 'expedite' : (create.lane ?? '');
  const types =
    create.type && !board.workTypes.includes(create.type) ? [...board.workTypes, create.type] : board.workTypes;
  const stations =
    create.station && !board.stations.includes(create.station) ? [...board.stations, create.station] : board.stations;
  // The lanes are the keys of the chosen station's route (factory.schema.json, station.route).
  const lanes = board.lanes[create.station] ?? [];
  const trustInfo = isCheck
    ? 'Work a check starts is merged by a person. Turn this on only for a source whose text you trust.'
    : 'Work a webhook starts is merged by a person. Turn this on only for a sender you trust, such as your own CI.';

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <TriggerWhen id={ids.when} value={value} onChange={onChange} problems={problems} />

      {isCheck ? (
        <CheckSection
          id={ids.check}
          value={value}
          onChange={onChange}
          board={board}
          problems={problems.check}
          memory={memory}
          onClear={onClear}
        />
      ) : (
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
        </Block>
      )}

      <Block id={ids.create} title="Create" problems={problems.create}>
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
        {isCheck ? null : (
          <>
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
              info="Text from the event reaches the agent marked as untrusted data, never as instructions."
            />
          </>
        )}
        <div className="flex items-center gap-3 text-body">
          <Switch
            aria-label="Merge without a person"
            checked={create.trusted === true}
            onCheckedChange={(on) => set('create', { ...create, trusted: on || undefined })}
          />
          <span>Merge without a person</span>
          <InfoTip label="About merging">{trustInfo}</InfoTip>
        </div>
      </Block>

      <Block
        id={ids.limits}
        title="Limits"
        info={isCheck ? 'Three failed checks in a row come to Needs you.' : undefined}
        problems={problems.limits}
      >
        {isCheck ? (
          <>
            <div className="flex flex-wrap items-center gap-2 text-body">
              <span>At most</span>
              <Count label="Items a check" value={limits.perCheck ?? 5} onChange={(n) => setLimits({ perCheck: n })} />
              <span>items a check and</span>
              <Count label="Items a day" value={limits.perDay ?? 20} onChange={(n) => setLimits({ perDay: n })} />
              <span>a day</span>
            </div>
            <div className="flex flex-wrap items-center gap-2 text-body">
              <span>Each check spends at most $</span>
              <Usd
                label="Most a check spends, in dollars"
                value={value.check?.maxUsd ?? 0.1}
                onChange={(n) => set('check', { agent: '', brief: '', ...value.check, maxUsd: n })}
              />
            </div>
          </>
        ) : (
          <div className="flex flex-col gap-3">
            <Option checked={limits.perDay !== undefined} onChange={(on) => setLimits({ perDay: on ? 3 : undefined })}>
              At most
              <Count
                label="Items a day"
                value={limits.perDay ?? 3}
                disabled={limits.perDay === undefined}
                onChange={(n) => setLimits({ perDay: n })}
              />
              items a day
            </Option>
            <Option
              checked={limits.perHour !== undefined}
              onChange={(on) => setLimits({ perHour: on ? 10 : undefined })}
            >
              At most
              <Count
                label="Items an hour"
                value={limits.perHour ?? 10}
                disabled={limits.perHour === undefined}
                onChange={(n) => setLimits({ perHour: n })}
              />
              items an hour
            </Option>
          </div>
        )}
        <Option
          checked={limits.pauseOnBudget === true}
          onChange={(on) => setLimits({ pauseOnBudget: on || undefined })}
        >
          Pause while {factoryName}&apos;s weekly budget is spent
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
  info,
}: {
  label: string;
  value: string;
  fields: string[];
  onChange: (v: string) => void;
  multiline: boolean;
  info?: string;
}) {
  const id = useId();
  const [focused, setFocused] = useState(false);
  const insert = (field: string) => onChange(`${value}{{${field}}}`);
  const common = 'rounded-cell border-rule bg-card px-3 py-2 text-body';
  // While the field is not being edited, its {{fields}} show as orange tokens over the transparent text.
  const marked = !focused && hasVariable(value);
  const markers = {
    onFocus: () => setFocused(true),
    onBlur: () => setFocused(false),
    className: cn(common, marked && 'text-transparent'),
  };
  return (
    <div className="flex flex-col gap-2">
      <span className="flex items-center gap-1 text-secondary font-semibold">
        <label htmlFor={id}>{label}</label>
        {info ? <InfoTip label={`About ${label.toLowerCase()}`}>{info}</InfoTip> : null}
      </span>
      <div className="relative">
        {multiline ? (
          <Textarea
            id={id}
            rows={4}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            {...markers}
            className={cn(markers.className, 'resize-none')}
          />
        ) : (
          <Input
            id={id}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            {...markers}
            className={cn('h-9', markers.className)}
          />
        )}
        {marked ? (
          <div
            aria-hidden="true"
            className={cn(
              'pointer-events-none absolute inset-0 overflow-hidden border border-transparent px-3 py-2 text-body text-ink',
              multiline ? 'whitespace-pre-wrap break-words' : 'flex items-center whitespace-nowrap',
            )}
          >
            <span>
              <TemplateText text={value} />
            </span>
          </div>
        ) : null}
      </div>
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
    </div>
  );
}
