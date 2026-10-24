// What every part of the Line section shares (D §10.6), provided once by LineSection: the line and
// what it refers to (agents, work types, the repository), what the running factory says about it
// in Definition (the constraint now, busy shares, open items per station), which stations are new
// in this draft (their id follows their name), and the edits rows make: a session edit, a move
// that keeps focus on the same control of the moved row and says where it went, and a station
// added with focus on its name.
import { useQuery } from '@tanstack/react-query';
import { createContext, type ReactNode, use, useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { boardQuery, draftQuery, settingsQuery, versionModelQuery } from '../../../api/queries';
import type { Board } from '../../../api/types';
import { type LineSection, type ModelStation, modelOf } from '../../../shared/definition/model';
import { type SlotCap, slotCap } from '../../../shared/definition/slots';
import type { AnyEdit, Edit } from '../draft/ops';
import { moveStation } from '../draft/ops';
import type { EditOptions } from '../draft/session';
import { useDraft, useDraftSession, useSection } from '../draft/use-draft';
import { stationLabel } from './labels';
import { movedMessage } from './reorder';

export interface LineEditor {
  place: 'definition' | 'wizard';
  line: LineSection;
  stations: ModelStation[];
  /** The agents' names, in the Agents section's order. */
  agents: string[];
  workTypes: string[];
  /** The factory has a repository (a URL or a connection): a merge step is possible. */
  hasRepository: boolean;
  /** The constraint the running line reports now (Definition only). */
  constraintNow: string | undefined;
  /** The constraint the line marks: the configured one, else the one now. */
  constraint: string | undefined;
  board: Board | undefined;
  slots: SlotCap | undefined;
  /** The station is absent from the version the draft is based on: its id follows its name. */
  isNew: (id: string) => boolean;
  /** The station as the base version has it; undefined for a new one. */
  baseOf: (id: string) => ModelStation | undefined;
  /** A stable React key per station, kept when a new station's id follows its name. */
  keyOf: (id: string) => string;
  /** Records that a station's id changed (a new station renamed), so its row keeps its key. */
  renamed: (from: string, to: string) => void;
  edit: (edits: AnyEdit | AnyEdit[], opts?: EditOptions) => void;
  flush: () => void;
  /** Moves a station to `to`, says so in the live region and keeps focus where it was in the row. */
  move: (id: string, to: number) => void;
  /** Adds a station (an edit from addStation/duplicateStation) and focuses its name. */
  insert: (edit: Edit<'line'>, station: ModelStation, then?: (line: LineSection) => Edit<'line'> | undefined) => void;
  /** The polite live region's text. */
  announcement: string;
  announce: (text: string) => void;
  /** The row that last had focus: Add station inserts after it. */
  lastFocused: React.RefObject<string | undefined>;
}

const LineEditorContext = createContext<LineEditor | null>(null);

export function useLineEditor(): LineEditor {
  const ctx = use(LineEditorContext);
  if (!ctx) throw new Error('useLineEditor outside LineEditorProvider');
  return ctx;
}

const focusable = 'input, select, textarea, button, [tabindex]:not([tabindex="-1"])';

/** The index of the focused control among a row's focusable controls, to focus it again after a move. */
function focusedControl(id: string): number {
  const row = document.querySelector(`[data-station="${CSS.escape(id)}"]`);
  const active = document.activeElement;
  if (!row || !active || !row.contains(active)) return -1;
  return [...row.querySelectorAll(focusable)].indexOf(active);
}

export function LineEditorProvider({ place, children }: { place: 'definition' | 'wizard'; children: ReactNode }) {
  const session = useDraftSession();
  const line = useSection('line');
  const agentsSection = useSection('agents');
  const basics = useSection('basics');
  const hasDraft = useDraft((s) => s.hasDraft);
  const { org, factory } = session;

  const draft = useQuery(draftQuery(org, factory)).data;
  // The version the draft is based on, by number (a published version never changes); without a
  // draft the editor shows the running version, which is then the base.
  const baseNumber = draft?.base?.number;
  const baseRef = hasDraft ? (baseNumber === undefined ? undefined : String(baseNumber)) : 'current';
  const base = useQuery({
    ...versionModelQuery(org, factory, baseRef ?? 'current'),
    enabled: baseRef !== undefined,
  }).data;
  const board = useQuery({ ...boardQuery(org, factory), enabled: place === 'definition' }).data;
  const settings = useQuery(settingsQuery(org)).data;

  const stations = line.stations ?? [];
  const baseStations = useMemo(
    () => new Map((base ? (modelOf(base.model).line.stations ?? []) : []).map((s) => [s.id, s])),
    [base],
  );
  // A never-published draft has no base: every station is new. Until the base loads, none is.
  const neverPublished = hasDraft && draft !== undefined && draft.base === null;
  const isNew = useCallback(
    (id: string) => (neverPublished ? true : base !== undefined && !baseStations.has(id)),
    [neverPublished, base, baseStations],
  );
  const baseOf = useCallback(
    (id: string) => (neverPublished ? undefined : baseStations.get(id)),
    [neverPublished, baseStations],
  );

  const keys = useRef(new Map<string, string>());
  const keyOf = useCallback((id: string) => keys.current.get(id) ?? id, []);
  const renamed = useCallback((from: string, to: string) => {
    keys.current.set(to, keys.current.get(from) ?? from);
    keys.current.delete(from);
  }, []);

  const [announcement, announce] = useState('');
  const lastFocused = useRef<string | undefined>(undefined);
  const refocus = useRef<{ id: string; control: number | 'name' } | null>(null);

  const edit = useCallback((edits: AnyEdit | AnyEdit[], opts?: EditOptions) => session.edit(edits, opts), [session]);
  const flush = useCallback(() => void session.flush(), [session]);

  const move = useCallback(
    (id: string, to: number) => {
      const e = moveStation(line, id, to);
      const station = stations.find((s) => s.id === id);
      if (!e || !station) return;
      refocus.current = { id, control: focusedControl(id) };
      session.edit(e);
      announce(movedMessage(stationLabel(station), to, stations.length));
    },
    [line, stations, session],
  );

  const insert = useCallback(
    (e: Edit<'line'>, station: ModelStation, then?: (line: LineSection) => Edit<'line'> | undefined) => {
      const placed = then?.(e.value) ?? e;
      session.edit(placed);
      const all = placed.value.stations ?? [];
      const at = all.findIndex((s) => s.id === station.id);
      refocus.current = { id: station.id, control: 'name' };
      announce(`${stationLabel(station)} added at position ${at + 1} of ${all.length}`);
    },
    [session],
  );

  // After a move or an add, the row's control takes focus again (React may have moved its node).
  useLayoutEffect(() => {
    const want = refocus.current;
    if (!want) return;
    refocus.current = null;
    const row = document.querySelector(`[data-station="${CSS.escape(want.id)}"]`);
    if (!row) return;
    const target =
      want.control === 'name'
        ? row.querySelector<HTMLElement>('[data-station-name]')
        : want.control >= 0
          ? row.querySelectorAll<HTMLElement>(focusable)[want.control]
          : undefined;
    if (target && document.activeElement !== target) target.focus();
    if (want.control === 'name' && target instanceof HTMLInputElement) target.select();
    if (target) target.scrollIntoView({ block: 'nearest' });
  });

  const repository = basics.repository;
  const hasRepository = Boolean(repository && (repository.url || repository.connection));
  const constraintNow = board?.constraint ?? undefined;
  const constraint = line.flow?.constraint ?? constraintNow;

  const value: LineEditor = {
    place,
    line,
    stations,
    agents: (agentsSection.agents ?? []).map((a) => a.name),
    workTypes: basics.work_types ?? [],
    hasRepository,
    constraintNow,
    constraint,
    board,
    slots: slotCap(settings?.slots, factory),
    isNew,
    baseOf,
    keyOf,
    renamed,
    edit,
    flush,
    move,
    insert,
    announcement,
    announce,
    lastFocused,
  };
  return <LineEditorContext value={value}>{children}</LineEditorContext>;
}
