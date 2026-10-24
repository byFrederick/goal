// What the editor frame says (D §10.3), as pure functions of the draft and the session: the
// section tiles, the status line's sentences, the conflict dialog's rows and where "1 problem"
// leads. The components in this folder only lay these out.
import type { DefinitionProblem, LineRange, ParseError, Section, SectionSummary } from '../../../api/types';
import type { Model } from '../../../shared/definition/model';
import { type Place, placeOf } from '../../../shared/definition/place';
import { formatClock, humanize, plural } from '../../../shared/format';
import type { Conflict } from '../draft/merge';
import type { SaveStatus } from '../draft/session';
import { linesText, sectionBlocks } from '../yaml/blocks';

export type TileState = 'problem' | 'edited' | 'unchanged';

/** A section tile's state and subtitle: "1 change, 1 problem", "2 changes", "No changes". */
export function sectionTile(summary: SectionSummary | undefined): { state: TileState; text: string } {
  const changes = summary?.changes ?? 0;
  const problems = summary?.problems ?? 0;
  if (problems > 0) {
    const text =
      changes > 0 ? `${plural(changes, 'change')}, ${plural(problems, 'problem')}` : plural(problems, 'problem');
    return { state: 'problem', text };
  }
  if (changes > 0) return { state: 'edited', text: plural(changes, 'change') };
  return { state: 'unchanged', text: 'No changes' };
}

/**
 * A section's tile in a view-only editor (canvas Define-View): what the section holds, since
 * there are no changes to count ("Purpose and repository", "7 stations").
 */
export function viewTileText(section: Exclude<Section, 'review'>, model: Model): string {
  switch (section) {
    case 'basics':
      return 'Purpose and repository';
    case 'line':
      return plural(model.line.stations?.length ?? 0, 'station');
    case 'agents':
      return plural(model.agents.agents?.length ?? 0, 'agent');
    case 'connections':
      return plural(model.connections.length, 'connection');
  }
}

/** The Review and publish tile: what stands between the draft and a publish. */
export function reviewTile({ problems, changes, hasDraft }: { problems: number; changes: number; hasDraft: boolean }): {
  state: TileState | 'ready';
  text: string;
} {
  if (problems > 0) return { state: 'problem', text: `Fix ${plural(problems, 'problem')} first` };
  if (hasDraft && changes > 0) return { state: 'ready', text: 'Ready to publish' };
  return { state: 'unchanged', text: 'No changes' };
}

/** Every tile while factory.yaml does not parse: Visual waits for it, Review cannot publish. */
export function parseErrorTile(section: Section, error: ParseError): { state: TileState; text: string } {
  if (section === 'review') return { state: 'problem', text: 'Fix factory.yaml first' };
  return { state: 'problem', text: error.line > 0 ? `Syntax error, line ${error.line}` : 'Syntax error' };
}

/** A tile in YAML mode jumps to its block: "board, lines 31–45". */
export function yamlTileText(section: Section, lines: LineRange | undefined): string {
  return `${sectionBlocks[section]}, ${linesText(lines ?? [0, 0])}`;
}

/** "3 changes in 2 sections"; null without changes. */
export function changesSentence(changeCount: number, summaries: readonly SectionSummary[]): string | null {
  if (changeCount <= 0) return null;
  const touched = summaries.filter((s) => s.changes > 0).length;
  return `${plural(changeCount, 'change')} in ${plural(Math.max(touched, 1), 'section')}`;
}

interface VersionNumber {
  number: number;
}

/** What the draft started from and what runs; `differs` after a restore (link to the differences). */
export function baseSentence({
  hasDraft,
  base,
  current,
}: {
  hasDraft: boolean;
  base: VersionNumber | null | undefined;
  current: VersionNumber | null | undefined;
}): { text: string; differs?: true } {
  if (!hasDraft) {
    return current
      ? { text: `v${current.number} runs now. Your first change opens a draft.` }
      : { text: 'Nothing published yet. Your first change opens a draft.' };
  }
  if (!base) return { text: 'A first draft, never published.' };
  if (!current) return { text: `Based on v${base.number}.` };
  if (base.number === current.number) return { text: `Based on v${base.number}, the version running now.` };
  return { text: `Based on v${base.number}; v${current.number} is running.`, differs: true };
}

/** How saving goes: "Autosaved 14:32.", "Saving…", "Not saved: … Retrying in 5 s."; null before any save. */
export function saveSentence({
  status,
  savedAt,
  retryAt,
  error,
  now,
}: {
  status: SaveStatus;
  savedAt: string | null;
  retryAt: number | null;
  error: { detail?: string | undefined; title: string } | null;
  now: number;
}): string | null {
  const retry = retryAt === null ? '' : ` Retrying in ${Math.max(1, Math.ceil((retryAt - now) / 1000))} s.`;
  switch (status) {
    case 'saving':
      return 'Saving…';
    case 'offline':
      return `Not saved: goald can’t be reached.${retry}`;
    case 'retrying':
      return `Not saved: goald failed.${retry}`;
    case 'conflict':
      return 'Not saved: the same field changed in another tab or the CLI.';
    case 'held':
      return 'Not saved: factory.yaml doesn’t parse. Visual changes wait until it does.';
    case 'failed':
      return `Not saved: ${(error?.detail ?? error?.title ?? 'goald refused the change').replace(/\.$/, '')}.`;
    default:
      return savedAt ? `Autosaved ${formatClock(savedAt, new Date(now))}.` : null;
  }
}

/** How long "Updated elsewhere at 14:35" stays in the status line. */
export const elsewhereMs = 10_000;

// The conflict dialog.

/** A field's name in a conflict row; others are their key as words. */
const fieldLabels: Record<string, string> = {
  wip: 'WIP limit',
  agent: 'agent',
  agents: 'agents',
  route: 'lanes',
  checks: 'checks',
  gate: 'rework rule',
  'gate on_reject': 'rework station',
  'gate max_rework': 'rework limit',
  action: 'merge step',
  work_types: 'work types',
  'repository url': 'repository',
  repository: 'repository',
  max_turns: 'max turns',
  per_item_usd: 'cap per item',
  default_model: 'default model',
  'budget weekly_usd': 'weekly budget',
  'budget on_exhausted': 'when the budget runs out',
  builtins: 'built-in tools',
};

/** The lists whose items the merge matches by id or name: their entity names a row. */
const entityLists = new Set(['stations', 'agents', 'skills', 'intake']);

export interface ConflictRow {
  pointer: string;
  /** "Build’s WIP limit", "Title". */
  label: string;
  theirs: string;
  ours: string;
}

function show(value: unknown): string {
  if (value === undefined) return 'removed';
  if (value === null) return 'none';
  if (typeof value === 'string') return value === '' ? 'empty' : value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  if (Array.isArray(value) && value.every((v) => typeof v !== 'object' || v === null)) {
    return value.length === 0 ? 'none' : value.join(', ');
  }
  const text = JSON.stringify(value);
  return text.length > 60 ? `${text.slice(0, 59)}…` : text;
}

const capitalize = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

function conflictLabel(section: Section, path: readonly string[]): string {
  let entity: string | undefined;
  let field = path;
  if (section === 'connections' && path.length > 1) [entity, ...field] = path;
  else if (path.length > 2 && entityLists.has(path[0] ?? '')) {
    entity = path[1];
    field = path.slice(2);
  }
  const key = field.join(' ');
  const name =
    fieldLabels[key] ?? fieldLabels[field.at(-1) ?? ''] ?? field.map((f) => f.replaceAll('_', ' ')).join(' ');
  return entity ? `${humanize(entity)}’s ${name}` : capitalize(name);
}

/** One row per field both sides changed, in the dialog's words. */
export function conflictRows(section: Section, conflicts: readonly Conflict[]): ConflictRow[] {
  return conflicts.map((c) => ({
    pointer: c.pointer,
    label: conflictLabel(section, c.path),
    theirs: show(c.theirs),
    ours: show(c.ours),
  }));
}

// "1 problem".

/** Where a problem is fixed: its section and entity; problems in files are listed on Review. */
export function problemPlace(problem: DefinitionProblem, model: Model): Place | { section: 'review' } {
  const place = problem.pointer ? placeOf(model, problem.pointer) : undefined;
  if (place) return place;
  const section = problem.section as Section;
  return ['basics', 'line', 'agents', 'connections', 'review'].includes(section) ? { section } : { section: 'review' };
}

/** A place as the section route's search params. */
export function placeSearch(place: Place | { section: 'review' }): {
  station?: string;
  agent?: string;
  connection?: string;
} {
  const { section: _section, ...search } = place as Place;
  return search;
}
