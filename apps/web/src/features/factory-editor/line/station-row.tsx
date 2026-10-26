// One station of the line (D §10.6): its number (the drag handle), name and id, kind, WIP limit,
// who works there, ↑ ↓ and More, then its detail strip and B's problems with their fixes. Six
// columns once the list is 960 px wide; below that the row stacks (name; kind; WIP and who; the
// order buttons). Alt+ArrowUp/Down anywhere in the row moves the station.
import { ArrowDown, ArrowUp, GripVertical, MoreHorizontal, TriangleAlert } from 'lucide-react';
import { type KeyboardEvent, useId, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../../api/client';
import type { ModelStation, StationKind } from '../../../shared/definition/model';
import { Button } from '../../../ui/button';
import { Chip } from '../../../ui/chip';
import { cn } from '../../../ui/cn';
import { ConfirmDialog } from '../../../ui/confirm-dialog';
import { readOnlyControl } from '../../../ui/field';
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from '../../../ui/menu';
import { SegmentedControl } from '../../../ui/segmented-control';
import {
  type AnyEdit,
  addStation,
  applyFix,
  duplicateStation,
  moveStation,
  removeStation,
  setStationField,
  setStationKind,
  setStationName,
} from '../draft/ops';
import { useDraftSession } from '../draft/use-draft';
import { useEdited, useProblems } from '../frame/use-editor';
import { kindLabel, stationLabel } from './labels';
import { useLineEditor } from './line-editor';
import { type Direction, stepBlocked, stepTarget } from './reorder';
import { NumberDialog, TextDialog } from './station-dialogs';
import { useStationDrag } from './station-dnd';
import { fieldOf, type StationField, StationRules } from './station-rules';
import { WhoWorks } from './who-works';

const kinds: StationKind[] = ['queue', 'agent', 'human', 'done'];

/** The words for the fields a kind change drops, for its toast. */
const fieldWords: Record<string, string> = {
  agent: 'its agent',
  route: 'its lanes',
  agents: 'parallel agents',
  wip: 'the WIP limit',
  buffer: 'the buffer',
  output: 'the output contract',
  checks: 'its checks',
  gate: 'the rework rule',
  action: 'the merge step',
};

const and = (words: string[]) =>
  words.length < 2 ? (words[0] ?? '') : `${words.slice(0, -1).join(', ')} and ${words.at(-1)}`;

export const lineColumns =
  '@[960px]:grid-cols-[32px_minmax(170px,1.1fr)_268px_132px_minmax(170px,1fr)_124px] @[960px]:gap-x-3';

const iconButton =
  'inline-flex size-11 shrink-0 items-center justify-center rounded-control border border-line bg-surface text-ink transition-colors duration-200 hover:bg-shell md:size-9 aria-disabled:cursor-not-allowed aria-disabled:opacity-45 aria-disabled:hover:bg-surface';

export function StationRow({ station, index, selected }: { station: ModelStation; index: number; selected: boolean }) {
  const editor = useLineEditor();
  const session = useDraftSession();
  const { line, stations } = editor;
  const id = useId();
  const n = index + 1;
  const label = stationLabel(station);
  const at = `/line/stations/${index}`;
  const isNew = editor.isNew(station.id);
  const last = index === stations.length - 1;
  const drag = useStationDrag(station);
  const problems = useProblems(at).filter((p) => p.severity !== 'info');
  const newHere = useEdited(at).includes(at);
  const [dialog, setDialog] = useState<'agents' | 'output' | 'buffer' | 'delete' | null>(null);

  const problemId = `${id}-problems`;
  const fields = new Set(problems.map((p) => fieldOf(p.pointer, index)));
  const invalid = (f: StationField) => fields.has(f);
  const describedBy = (f: StationField) => (fields.has(f) ? problemId : undefined);
  // A problem about no one field (the station as a whole) is described from its name.
  const nameDescribedBy = [
    `${id}-id`,
    fields.has('name') || fields.has('other') || fields.has('note') ? problemId : null,
  ]
    .filter(Boolean)
    .join(' ');

  const step = (dir: Direction) => {
    const to = stepTarget(stations, station.id, dir);
    if (to !== undefined) editor.move(station.id, to);
  };

  const onKeyDownCapture = (e: KeyboardEvent<HTMLLIElement>) => {
    if (!e.altKey || (e.key !== 'ArrowUp' && e.key !== 'ArrowDown')) return;
    e.preventDefault();
    e.stopPropagation();
    step(e.key === 'ArrowUp' ? -1 : 1);
  };

  const setName = (name: string) => {
    const e = setStationName(line, station.id, name, { followId: isNew });
    const next = e.value.stations?.[index]?.id;
    if (next && next !== station.id) editor.renamed(station.id, next);
    editor.edit(e, { coalesce: `${editor.keyOf(station.id)}/name` });
  };

  const setKind = (kind: StationKind) => {
    const { edit, dropped } = setStationKind(line, station.id, kind);
    editor.edit(edit);
    if (dropped.length > 0) {
      toast(`${label} is a ${kind} station now. Removed ${and(dropped.map((d) => fieldWords[d] ?? d))}.`, {
        action: { label: 'Undo', onClick: () => session.undo() },
      });
    }
  };

  const setWip = (wip: number | undefined) =>
    editor.edit(setStationField(line, station.id, 'wip', wip), { coalesce: `${editor.keyOf(station.id)}/wip` });

  const insert = (where: 'above' | 'below') => {
    const { edit, station: added } = addStation(line, 'agent', {
      name: 'New station',
      ...(where === 'below' ? { after: station.id } : {}),
    });
    editor.insert(edit, added, where === 'above' ? (l) => moveStation(l, added.id, index) : undefined);
  };

  const fix = (fixEdit: AnyEdit) => editor.edit(fixEdit);
  const modelNow = () => {
    const s = session.getSnapshot().sections;
    return {
      basics: s.basics.local,
      line: s.line.local,
      agents: s.agents.local,
      connections: s.connections.local,
      review: s.review.local,
    };
  };

  const busy = editor.board?.stations.find((s) => s.id === station.id)?.busy_ratio;
  const openItems = (() => {
    const s = editor.board?.stations.find((x) => x.id === station.id);
    return s ? s.waiting + s.in_progress : 0;
  })();
  const isConstraint = editor.place === 'definition' && editor.constraint === station.id;
  const up = stepBlocked(stations, station.id, -1);
  const down = stepBlocked(stations, station.id, 1);

  return (
    <li
      ref={drag.setNodeRef}
      data-station={station.id}
      aria-label={`${n}. ${label}, ${station.kind} station`}
      onKeyDownCapture={onKeyDownCapture}
      onFocus={() => {
        editor.lastFocused.current = station.id;
      }}
      className={cn(
        'grid grid-cols-[32px_minmax(0,1fr)] items-center gap-x-2.5 gap-y-2.5 rounded-control border bg-surface p-3 transition-[border-color,box-shadow,opacity] duration-200 @[960px]:px-4 @[960px]:py-3.5',
        lineColumns,
        selected ? 'border-ink shadow-[0_0_0_1px_var(--color-ink)]' : 'border-line focus-within:border-rule-strong',
        problems.length > 0 && !selected && 'border-rework focus-within:border-rework',
        drag.over && 'border-run shadow-[0_0_0_1px_var(--color-run)]',
        drag.dragging && 'opacity-50',
      )}
    >
      {drag.handle && !editor.viewOnly ? (
        <button
          type="button"
          {...drag.handle}
          aria-label={`Drag ${label}, station ${n} of ${stations.length}`}
          className="group/handle flex h-11 w-8 touch-none items-center justify-center rounded-chip bg-shell text-[13px] font-semibold tabular-nums transition-colors duration-200 hover:bg-control md:h-7 md:w-7 md:cursor-grab"
        >
          <span className="group-hover/handle:hidden">{n}</span>
          <GripVertical size={14} strokeWidth={1.5} aria-hidden="true" className="hidden group-hover/handle:block" />
        </button>
      ) : (
        <span
          aria-hidden="true"
          className="flex h-11 w-8 items-center justify-center rounded-chip bg-shell text-[13px] font-semibold tabular-nums md:h-7 md:w-7"
        >
          {n}
        </span>
      )}

      <div className="flex min-w-0 flex-col gap-1">
        <input
          data-station-name=""
          value={station.name ?? ''}
          aria-label={`Name of station ${n}`}
          aria-describedby={nameDescribedBy}
          aria-invalid={invalid('name') || undefined}
          onChange={(e) => setName(e.target.value)}
          onBlur={editor.flush}
          readOnly={editor.viewOnly}
          autoComplete="off"
          placeholder={station.id}
          className={cn(
            'h-11 w-full min-w-0 rounded-control border border-rule bg-surface px-3 text-base text-ink transition-colors duration-200 placeholder:text-muted hover:border-faint aria-[invalid=true]:border-rework md:h-9 md:text-[15px]',
            readOnlyControl,
          )}
        />
        <span className="group/id flex flex-wrap items-center gap-1.5">
          <span translate="no" className="font-mono text-xs text-muted">
            id: {station.id}
          </span>
          {isConstraint ? (
            <Chip tone="constraint" className="h-5 text-[11px]">
              {typeof busy === 'number' ? `Constraint, busy ${Math.round(busy * 100)}%` : 'Constraint'}
            </Chip>
          ) : null}
          {newHere ? (
            <Chip tone="ok" className="h-5 border-ok-tint bg-ok-tint text-[11px] text-ok-ink">
              New<span className="sr-only"> in this draft</span>
            </Chip>
          ) : (
            <RowEdited index={index} label={label} />
          )}
          <span id={`${id}-id`} className="sr-only">
            {isNew
              ? 'The id follows the name until this station is published.'
              : 'The id stays the same so items keep their place.'}
          </span>
        </span>
      </div>

      <div className="col-span-full min-w-0 @[960px]:col-span-1">
        <SegmentedControl
          label={`Kind of ${label}`}
          className="flex w-full [&>label]:min-w-0 [&>label]:flex-1 [&>label]:justify-center"
          value={station.kind}
          onChange={setKind}
          readOnly={editor.viewOnly}
          options={kinds.map((k) => ({
            value: k,
            label: kindLabel[k],
            disabledReason:
              k === 'done' && !last && station.kind !== 'done' ? 'Only the last station can be Done' : undefined,
          }))}
        />
      </div>

      <div className="col-span-full flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 @[960px]:contents">
        <div className="min-w-0">
          {station.kind === 'agent' || station.kind === 'human' ? (
            <WipStepper
              label={label}
              wip={station.wip}
              onChange={setWip}
              onBlur={editor.flush}
              invalid={invalid('wip')}
              describedBy={describedBy('wip')}
              readOnly={editor.viewOnly}
            />
          ) : (
            <span className="text-sm text-muted">Not limited</span>
          )}
        </div>
        <div className="min-w-[130px] flex-1 @[960px]:min-w-0">
          <WhoWorks station={station} invalid={invalid('who')} describedBy={describedBy('who')} />
        </div>
        {/* Beside WIP and who works when the row has room, on a line of their own when not. */}
        {editor.viewOnly ? null : (
          <div className="ml-auto flex justify-end gap-1.5">
            <OrderButton dir={-1} label={label} blocked={up} onStep={step} />
            <OrderButton dir={1} label={label} blocked={down} onStep={step} />
            <Menu>
              <MenuTrigger asChild>
                <button type="button" aria-label={`More for ${label}`} className={iconButton}>
                  <MoreHorizontal size={16} strokeWidth={1.5} aria-hidden="true" />
                </button>
              </MenuTrigger>
              <MenuContent side="bottom" align="end" className="w-64">
                <MenuItem onSelect={() => insert('above')}>Insert above</MenuItem>
                {station.kind === 'done' ? null : <MenuItem onSelect={() => insert('below')}>Insert below</MenuItem>}
                {station.kind === 'done' ? null : (
                  <MenuItem
                    onSelect={() => {
                      const copy = duplicateStation(line, station.id);
                      if (copy) editor.insert(copy.edit, copy.station);
                    }}
                  >
                    Duplicate
                  </MenuItem>
                )}
                {station.kind === 'agent' ? (
                  <>
                    <MenuSeparator />
                    <MenuItem onSelect={() => setDialog('agents')}>Parallel agents…</MenuItem>
                    <MenuItem onSelect={() => setDialog('output')}>Output contract…</MenuItem>
                    <MenuItem onSelect={() => setDialog('buffer')}>Buffer in front…</MenuItem>
                  </>
                ) : null}
                <MenuSeparator />
                <MenuItem onSelect={() => setDialog('delete')} className="text-rework-ink">
                  Delete…
                </MenuItem>
              </MenuContent>
            </Menu>
          </div>
        )}
      </div>

      <StationRules station={station} index={index} invalid={invalid} describedBy={describedBy} />

      {problems.length > 0 ? (
        <ul id={problemId} className="col-span-full m-0 flex list-none flex-col gap-2 p-0 @[960px]:col-start-2">
          {problems.map((p) => (
            <li key={`${p.code}-${p.pointer}`} className="flex flex-col gap-1.5 text-[13px] leading-snug">
              <span className="flex items-start gap-1.5 text-rework-ink">
                <TriangleAlert size={14} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0" />
                <span>
                  <span className="sr-only">{p.severity === 'error' ? 'Problem: ' : 'Warning: '}</span>
                  {p.message}
                </span>
              </span>
              {p.fixes.length > 0 && !editor.viewOnly ? (
                <span className="flex flex-wrap gap-1.5 pl-5">
                  {p.fixes.map((f) => (
                    <Button
                      key={f.label}
                      variant="white"
                      size="sm"
                      className="h-11 md:h-8"
                      onClick={() => fix(applyFix(modelNow(), f))}
                    >
                      {f.label}
                    </Button>
                  ))}
                </span>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}

      <NumberDialog
        open={dialog === 'agents'}
        onOpenChange={(o) => setDialog(o ? 'agents' : null)}
        title={`Parallel agents at ${label}`}
        description="How many copies of the agent work here at once, each on its own item, up to the WIP limit."
        label="Agents"
        help="From 1 to 20."
        initial={station.agents ?? 1}
        min={1}
        max={20}
        onSave={(v) => editor.edit(setStationField(line, station.id, 'agents', v === 1 ? undefined : v))}
      />
      <TextDialog
        open={dialog === 'output'}
        onOpenChange={(o) => setDialog(o ? 'output' : null)}
        title={`Output contract of ${label}`}
        description="The contract the agent's output must match before work moves on. Contracts live in contracts/."
        label="Contract"
        help="Its name in contracts/, as in spec. Leave it empty for none."
        initial={station.output ?? ''}
        submit="Save"
        mono
        check={(v) =>
          v === '' || /^[a-z0-9][a-z0-9._-]*$/.test(v) ? undefined : 'Use the contract’s file name, as in spec.'
        }
        onSave={(v) => editor.edit(setStationField(line, station.id, 'output', v || undefined))}
      />
      <NumberDialog
        open={dialog === 'buffer'}
        onOpenChange={(o) => setDialog(o ? 'buffer' : null)}
        title={`Buffer in front of ${label}`}
        description="How many items to keep ready in front of this station when the line releases by drum-buffer-rope."
        label="Items"
        help="Leave it empty for no buffer."
        initial={station.buffer}
        min={1}
        clearable
        onSave={(v) => editor.edit(setStationField(line, station.id, 'buffer', v))}
      />
      <ConfirmDialog
        open={dialog === 'delete'}
        onOpenChange={(o) => setDialog(o ? 'delete' : null)}
        title={`Delete ${label}?`}
        description={
          openItems > 0
            ? `${openItems === 1 ? '1 item is' : `${openItems} items are`} at ${label} now. Publishing without it asks where they go.`
            : 'Gates that send work back here and the constraint, if it is this station, go with it.'
        }
        confirmLabel="Delete station"
        tone="danger"
        onConfirm={() => {
          editor.edit(removeStation(line, station.id));
          editor.announce(`${label} deleted`);
          setDialog(null);
        }}
      />
    </li>
  );
}

function OrderButton({
  dir,
  label,
  blocked,
  onStep,
}: {
  dir: Direction;
  label: string;
  blocked: string | undefined;
  onStep: (dir: Direction) => void;
}) {
  const id = useId();
  const Icon = dir === -1 ? ArrowUp : ArrowDown;
  return (
    <>
      <button
        type="button"
        aria-label={`Move ${label} ${dir === -1 ? 'up' : 'down'}`}
        aria-disabled={blocked ? true : undefined}
        aria-describedby={blocked ? id : undefined}
        aria-keyshortcuts={dir === -1 ? 'Alt+ArrowUp' : 'Alt+ArrowDown'}
        title={blocked}
        onClick={() => {
          if (!blocked) onStep(dir);
        }}
        className={iconButton}
      >
        <Icon size={16} strokeWidth={1.5} aria-hidden="true" />
      </button>
      {blocked ? (
        <span id={id} className="sr-only">
          {blocked}
        </span>
      ) : null}
    </>
  );
}

function WipStepper({
  label,
  wip,
  onChange,
  onBlur,
  invalid,
  describedBy,
  readOnly,
}: {
  label: string;
  wip: number | undefined;
  onChange: (wip: number | undefined) => void;
  onBlur: () => void;
  invalid: boolean;
  describedBy: string | undefined;
  /** The limit only: no − and + (a view-only editor). */
  readOnly: boolean;
}) {
  const atMin = (wip ?? 1) <= 1;
  return (
    <fieldset className="m-0 inline-flex min-w-0 items-center gap-1 border-0 p-0">
      <legend className="sr-only">WIP limit for {label}</legend>
      {readOnly ? null : (
        <button
          type="button"
          aria-label={`Lower the WIP limit of ${label}`}
          aria-disabled={atMin || undefined}
          onClick={() => {
            if (!atMin) onChange((wip ?? 2) - 1);
          }}
          className={iconButton}
        >
          <span aria-hidden="true">−</span>
        </button>
      )}
      <input
        inputMode="numeric"
        value={wip ?? ''}
        aria-label={`WIP limit of ${label}`}
        aria-invalid={invalid || undefined}
        aria-describedby={describedBy}
        onChange={(e) => {
          const n = Number.parseInt(e.target.value.replace(/[^0-9]/g, ''), 10);
          onChange(Number.isInteger(n) && n > 0 ? n : undefined);
        }}
        onBlur={onBlur}
        readOnly={readOnly}
        autoComplete="off"
        className={cn(
          'h-11 w-12 rounded-control border border-rule bg-surface text-center text-base text-ink tabular-nums transition-colors duration-200 placeholder:text-muted hover:border-faint aria-[invalid=true]:border-rework md:h-9 md:text-[15px]',
          readOnlyControl,
        )}
      />
      {readOnly ? null : (
        <button
          type="button"
          aria-label={`Raise the WIP limit of ${label}`}
          onClick={() => onChange((wip ?? 0) + 1)}
          className={iconButton}
        >
          <span aria-hidden="true">+</span>
        </button>
      )}
    </fieldset>
  );
}

/**
 * "Edited · Undo" for the row's own fields (name, kind, WIP limit, who works there): one mark for
 * all of them, whose Undo puts the base version's values back through goald.
 */
function RowEdited({ index, label }: { index: number; label: string }) {
  const session = useDraftSession();
  const at = `/line/stations/${index}`;
  const own = new Set(['name', 'kind', 'wip', 'agent', 'agents', 'route'].map((f) => `${at}/${f}`));
  const edited = useEdited(at).filter((p) => own.has(p));
  if (edited.length === 0) return null;
  const fields = and(edited.map((p) => fieldName[p.split('/').at(-1) ?? ''] ?? 'value'));
  const undo = () =>
    session.revert('line', edited).catch((err: unknown) => {
      toast.error(`Not undone: ${err instanceof ApiError ? (err.detail ?? err.title) : 'goald can’t be reached'}`);
    });
  return (
    <span className="inline-flex items-center gap-1.5 text-[13px]">
      <span className="inline-flex h-5 items-center rounded-chip bg-ok-tint px-1.5 text-[11px] font-medium text-ok-ink">
        Edited<span className="sr-only">: {fields}</span>
      </span>
      <button
        type="button"
        onClick={() => void undo()}
        aria-label={`Undo the changes to ${fields} of ${label}`}
        className="inline-flex min-h-6 items-center rounded-chip px-1 text-ink underline underline-offset-3 transition-colors duration-200 hover:text-run"
      >
        Undo
      </button>
    </span>
  );
}

const fieldName: Record<string, string> = {
  name: 'the name',
  kind: 'the kind',
  wip: 'the WIP limit',
  agent: 'the agent',
  agents: 'the parallel agents',
  route: 'the lanes',
};
