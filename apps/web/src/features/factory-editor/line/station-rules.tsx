// A station's detail strip (D §10.6), under its row: an agent station's checks and lanes, the
// rework rule of agent and human stations, the merge step of a human station and of an agent
// station with a gate, a queue's buffer sentence, and every station's note.
import { Link, useParams } from '@tanstack/react-router';
import { Plus, X } from 'lucide-react';
import { type RefObject, useId, useRef, useState } from 'react';
import type { ModelStation, StationCheck } from '../../../shared/definition/model';
import { Button } from '../../../ui/button';
import { cn } from '../../../ui/cn';
import { readOnlyControl } from '../../../ui/field';
import { Switch } from '../../../ui/switch';
import { addCheck, removeCheck, setGate, setMergeStep, setStationField } from '../draft/ops';
import { EditedMark } from '../frame/section-shell';
import { bufferSentence, checkLabel, stationLabel, upstreamOf } from './labels';
import { useLineEditor } from './line-editor';
import { AddCheckDialog, LanesDialog } from './station-dialogs';

/** Which of a row's controls a problem's pointer is about. */
export type StationField = 'name' | 'kind' | 'wip' | 'who' | 'gate' | 'action' | 'checks' | 'note' | 'other';

export function fieldOf(pointer: string | undefined, index: number): StationField {
  const rest = (pointer ?? '').slice(`/line/stations/${index}/`.length).split('/')[0] ?? '';
  switch (rest) {
    case 'name':
    case 'id':
      return 'name';
    case 'kind':
      return 'kind';
    case 'wip':
      return 'wip';
    case 'agent':
    case 'agents':
    case 'route':
      return 'who';
    case 'gate':
      return 'gate';
    case 'action':
      return 'action';
    case 'checks':
    case 'output':
      return 'checks';
    case 'description':
      return 'note';
    default:
      return 'other';
  }
}

const rule = 'flex min-w-0 flex-wrap items-center gap-2';
const smallInput = cn(
  'h-11 rounded-control border border-rule bg-card px-2 text-base text-ink transition-colors duration-200 hover:border-muted md:h-8 md:text-sm aria-[invalid=true]:border-rework',
  readOnlyControl,
  'disabled:cursor-default disabled:border-line disabled:bg-shell disabled:hover:border-line',
);

/** Where the Basics section is, in Definition or in the wizard. */
function BasicsLink({ children }: { children: string }) {
  const editor = useLineEditor();
  const params = useParams({ strict: false }) as { org?: string; factory?: string };
  const className = 'text-ink underline underline-offset-3 hover:text-run';
  if (editor.place === 'wizard') {
    return (
      <Link
        to="/o/$org/new/$step"
        params={{ org: params.org ?? '', step: 'basics' }}
        search={(prev) => (prev.factory ? { factory: prev.factory } : {})}
        className={className}
      >
        {children}
      </Link>
    );
  }
  return (
    <Link
      to="/o/$org/f/$factory/definition/$section"
      params={{ org: params.org ?? '', factory: params.factory ?? '', section: 'basics' }}
      className={className}
    >
      {children}
    </Link>
  );
}

export function StationRules({
  station,
  index,
  invalid,
  describedBy,
}: {
  station: ModelStation;
  index: number;
  invalid: (field: StationField) => boolean;
  describedBy: (field: StationField) => string | undefined;
}) {
  const editor = useLineEditor();
  const { line, stations } = editor;
  const label = stationLabel(station);
  const at = `/line/stations/${index}`;
  const kind = station.kind;
  const buffer = kind === 'queue' ? bufferSentence(line, station.id) : undefined;
  const [noting, setNoting] = useState(false);
  const noteInput = useRef<HTMLInputElement>(null);
  // A rework rule sends work back upstream: offered where there is somewhere to send it.
  const upstream = upstreamOf(stations, index);
  const reworkTarget =
    (kind === 'agent' || kind === 'human') && !station.gate
      ? (upstream.findLast((s) => s.kind === 'agent') ?? upstream[upstream.length - 1])
      : undefined;

  return (
    <div className="col-span-full flex min-w-0 flex-col gap-2 border-t border-dashed border-line pt-2.5 text-sm @[960px]:col-start-2">
      {buffer ? (
        <p className="m-0 text-muted">
          {buffer}. <span className="text-ink-soft">Not enforced yet; work is released as it arrives.</span>
        </p>
      ) : null}
      {kind === 'agent' && station.route && Object.keys(station.route).length > 0 ? (
        <Lanes station={station} index={index} />
      ) : null}
      {kind === 'agent' ? (
        <Checks station={station} index={index} invalid={invalid('checks')} describedBy={describedBy('checks')} />
      ) : null}
      {kind === 'agent' || kind === 'human' ? (
        <GateRule
          station={station}
          index={index}
          upstream={upstream}
          invalid={invalid('gate')}
          describedBy={describedBy('gate')}
        />
      ) : null}
      {kind === 'human' || (kind === 'agent' && station.gate) ? (
        <MergeStep
          station={station}
          gated={kind === 'agent'}
          invalid={invalid('action')}
          describedBy={describedBy('action')}
          at={at}
        />
      ) : null}
      {station.description !== undefined || noting ? (
        <Note station={station} label={label} at={at} input={noteInput} onDone={() => setNoting(false)} />
      ) : null}
      {!editor.viewOnly && (reworkTarget || (station.description === undefined && !noting)) ? (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
          {reworkTarget ? (
            <button
              type="button"
              onClick={() => editor.edit(setGate(line, station.id, { on_reject: reworkTarget.id, max_rework: 2 }))}
              aria-label={`Add a rework rule to ${label}`}
              className={addLink}
            >
              Add a rework rule
            </button>
          ) : null}
          {station.description === undefined && !noting ? (
            <button
              type="button"
              onClick={() => {
                setNoting(true);
                requestAnimationFrame(() => noteInput.current?.focus());
              }}
              aria-label={`Add a note to ${label}`}
              className={addLink}
            >
              Add a note
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

const addLink =
  'inline-flex min-h-11 items-center px-0.5 text-[13px] text-ink-soft underline underline-offset-3 transition-colors duration-200 hover:text-ink md:min-h-6';

function Lanes({ station, index }: { station: ModelStation; index: number }) {
  const editor = useLineEditor();
  const [open, setOpen] = useState(false);
  const label = stationLabel(station);
  return (
    <div className={rule}>
      <span className="text-muted">Routed by the spec’s lane</span>
      {Object.entries(station.route ?? {}).map(([lane, agent]) => (
        <span
          key={lane}
          className="inline-flex h-6 items-center gap-1.5 rounded-chip border border-line bg-card px-2 text-xs font-medium"
        >
          {lane} <span aria-hidden="true">→</span>
          <span className="sr-only"> goes to</span>{' '}
          <span translate="no" className="font-mono font-normal">
            {agent}
          </span>
        </span>
      ))}
      {editor.viewOnly ? null : (
        <button
          type="button"
          onClick={() => setOpen(true)}
          aria-haspopup="dialog"
          aria-label={`Edit lanes of ${label}`}
          className="inline-flex min-h-6 items-center px-0.5 text-[13px] text-ink-soft underline underline-offset-3 transition-colors duration-200 hover:text-ink"
        >
          Edit lanes
        </button>
      )}
      <EditedMark section="line" pointer={`/line/stations/${index}/route`} label={`the lanes of ${label}`} />
      <LanesDialog
        open={open}
        onOpenChange={setOpen}
        station={label}
        route={station.route}
        agents={editor.agents}
        suggestions={editor.workTypes}
        onSave={(route) => {
          const next = setStationField(editor.line, station.id, 'route', route ?? undefined);
          editor.edit(next);
        }}
      />
    </div>
  );
}

function Checks({
  station,
  index,
  invalid,
  describedBy,
}: {
  station: ModelStation;
  index: number;
  invalid: boolean;
  describedBy: string | undefined;
}) {
  const editor = useLineEditor();
  const [open, setOpen] = useState(false);
  const label = stationLabel(station);
  const checks = station.checks ?? [];
  const isSchema = (c: string | StationCheck) => typeof c !== 'string' && c.kind === 'schema';
  // A check the base version's station lacks was added in this draft (none is new on a new station).
  const base = editor.baseOf(station.id);
  const baseChecks = new Set((base?.checks ?? []).map((c) => checkLabel(c, base?.output)));
  const seen = new Map<string, number>();

  return (
    <div className={rule}>
      <span className="text-muted">Checks before it moves on</span>
      {checks.map((c, i) => {
        const text = checkLabel(c, station.output);
        const isNew = base !== undefined && !baseChecks.has(text);
        const nth = (seen.get(text) ?? 0) + 1;
        seen.set(text, nth);
        return (
          <span
            // Checks have no identity but their text (a repeated one, its occurrence).
            key={`${text}#${nth}`}
            className={cn(
              'inline-flex h-7 max-w-full items-center gap-1 rounded-chip border pr-0.5 pl-2 text-xs',
              isNew ? 'border-ok-tint bg-ok-tint text-ok-ink' : 'border-line bg-card',
            )}
          >
            <span translate="no" className="truncate font-mono">
              {text}
            </span>
            {isNew ? <span className="sr-only">, added in this draft</span> : null}
            {editor.viewOnly ? (
              <span className="w-1.5" />
            ) : (
              <button
                type="button"
                aria-label={`Remove check ${text} from ${label}`}
                onClick={() => editor.edit(removeCheck(editor.line, station.id, i))}
                className="inline-flex size-6 shrink-0 items-center justify-center rounded-chip text-muted transition-colors duration-200 hover:bg-shell hover:text-ink"
              >
                <X size={13} strokeWidth={1.5} aria-hidden="true" />
              </button>
            )}
          </span>
        );
      })}
      {editor.viewOnly && checks.length === 0 ? <span className="text-ink-soft">None</span> : null}
      {editor.viewOnly ? null : (
        <Button
          variant="white"
          size="sm"
          className="h-11 md:h-8"
          aria-haspopup="dialog"
          aria-label={`Add check to ${label}`}
          aria-invalid={invalid || undefined}
          aria-describedby={describedBy}
          onClick={() => setOpen(true)}
        >
          <Plus size={14} strokeWidth={1.5} aria-hidden="true" />
          Add check
        </Button>
      )}
      <EditedMark section="line" pointer={`/line/stations/${index}/checks`} label={`the checks of ${label}`} />
      <AddCheckDialog
        open={open}
        onOpenChange={setOpen}
        station={label}
        output={station.output}
        hasSchemaCheck={checks.some(isSchema)}
        onAdd={(check) => editor.edit(addCheck(editor.line, station.id, check))}
      />
    </div>
  );
}

function GateRule({
  station,
  index,
  upstream,
  invalid,
  describedBy,
}: {
  station: ModelStation;
  index: number;
  upstream: ModelStation[];
  invalid: boolean;
  describedBy: string | undefined;
}) {
  const editor = useLineEditor();
  const id = useId();
  const label = stationLabel(station);
  const gate = station.gate;

  if (!gate) return null;

  const known = upstream.some((s) => s.id === gate.on_reject);
  return (
    <div className={rule}>
      <label htmlFor={`${id}-to`} className="text-muted">
        If rejected, send back to
      </label>
      <select
        id={`${id}-to`}
        value={gate.on_reject ?? ''}
        aria-invalid={invalid || undefined}
        aria-describedby={describedBy}
        disabled={editor.viewOnly}
        onChange={(e) => editor.edit(setGate(editor.line, station.id, { ...gate, on_reject: e.target.value }))}
        className={cn(smallInput, 'max-w-[220px] cursor-pointer pr-7')}
      >
        {known ? null : <option value={gate.on_reject ?? ''}>{gate.on_reject || 'Choose a station'}</option>}
        {upstream.map((s) => (
          <option key={s.id} value={s.id}>
            {stationLabel(s)}
          </option>
        ))}
      </select>
      <label htmlFor={`${id}-max`} className="text-muted">
        at most
      </label>
      <input
        id={`${id}-max`}
        inputMode="numeric"
        value={gate.max_rework ?? ''}
        onChange={(e) => {
          const n = Number.parseInt(e.target.value.replace(/[^0-9]/g, ''), 10);
          const { max_rework: _old, ...rest } = gate;
          editor.edit(
            setGate(editor.line, station.id, Number.isInteger(n) && n > 0 ? { ...rest, max_rework: n } : rest),
            {
              coalesce: `${editor.keyOf(station.id)}/gate/max_rework`,
            },
          );
        }}
        onBlur={editor.flush}
        readOnly={editor.viewOnly}
        autoComplete="off"
        className={cn(smallInput, 'w-12 text-center tabular-nums')}
      />
      <span className="text-muted">times</span>
      {editor.viewOnly ? null : (
        <button
          type="button"
          onClick={() => editor.edit(setGate(editor.line, station.id, null))}
          aria-label={`Remove the rework rule of ${label}`}
          className="inline-flex size-11 items-center justify-center rounded-chip text-muted transition-colors duration-200 hover:bg-shell hover:text-ink md:size-7"
        >
          <X size={14} strokeWidth={1.5} aria-hidden="true" />
        </button>
      )}
      <EditedMark section="line" pointer={`/line/stations/${index}/gate`} label={`the rework rule of ${label}`} />
    </div>
  );
}

function MergeStep({
  station,
  gated,
  invalid,
  describedBy,
  at,
}: {
  station: ModelStation;
  /** An agent station with a gate: approving the gate merges, with no person. */
  gated: boolean;
  invalid: boolean;
  describedBy: string | undefined;
  at: string;
}) {
  const editor = useLineEditor();
  const id = useId();
  const on = station.action === 'merge';
  // Turning it off is always possible, so a merge step left without a repository can be undone.
  const blocked = !editor.hasRepository && !on;
  const what = on
    ? gated
      ? 'Approving the gate merges the pull request (no person)'
      : 'A person merges the pull request'
    : blocked
      ? 'Set a repository in Basics first'
      : gated
        ? 'Approving the gate leaves the pull request open'
        : 'People approve here; nothing is merged';

  return (
    <div className={rule}>
      <Switch
        checked={on}
        disabled={blocked}
        onCheckedChange={(v) => editor.edit(setMergeStep(editor.line, station.id, v))}
        aria-labelledby={`${id}-label`}
        aria-describedby={[`${id}-what`, describedBy].filter(Boolean).join(' ')}
        aria-invalid={invalid || undefined}
      />
      <span id={`${id}-label`} className="font-medium">
        Merge step<span className="sr-only"> at {stationLabel(station)}</span>
      </span>
      <span id={`${id}-what`} className="text-muted">
        {blocked ? (
          <>
            Set a repository in <BasicsLink>Basics</BasicsLink> first
          </>
        ) : (
          what
        )}
      </span>
      <EditedMark section="line" pointer={`${at}/action`} label={`the merge step of ${stationLabel(station)}`} />
    </div>
  );
}

function Note({
  station,
  label,
  at,
  input,
  onDone,
}: {
  station: ModelStation;
  label: string;
  at: string;
  input: RefObject<HTMLInputElement | null>;
  onDone: () => void;
}) {
  const editor = useLineEditor();
  const note = station.description;

  return (
    <div className={rule}>
      <input
        ref={input}
        value={note ?? ''}
        aria-label={`Note on ${label}`}
        placeholder="What happens here, in a sentence"
        onChange={(e) =>
          editor.edit(setStationField(editor.line, station.id, 'description', e.target.value || undefined), {
            coalesce: `${editor.keyOf(station.id)}/description`,
          })
        }
        onBlur={() => {
          if (!note) onDone();
          editor.flush();
        }}
        readOnly={editor.viewOnly}
        autoComplete="off"
        className="h-11 min-w-0 flex-1 rounded-control border border-transparent bg-transparent px-1.5 text-base text-muted transition-colors duration-200 placeholder:text-muted hover:border-rule focus-visible:border-rule focus-visible:bg-card focus-visible:text-ink md:h-8 md:text-sm"
      />
      <EditedMark section="line" pointer={`${at}/description`} label={`the note on ${label}`} />
    </div>
  );
}
