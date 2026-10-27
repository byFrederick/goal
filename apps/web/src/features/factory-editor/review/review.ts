// What Review and publish and Versions say (D §10.10), as pure functions: which comparison to
// show, the unified diff of one file with B's change labels on its hunks, the version note a
// publish starts from, the station moves a publish needs, and the words of version rows.
import { structuredPatch } from 'diff';
import type { Actor, Change, DefinitionProblem, VersionSource } from '../../../api/types';
import { plural } from '../../../shared/format';
import { sectionInfo } from '../sections';

// Comparisons.

export interface ComparePair {
  from: string;
  to: string;
}

/** `?compare=7..draft` → {from: '7', to: 'draft'}; undefined when absent or malformed. */
export function parseCompare(value: string | undefined): ComparePair | undefined {
  const m = value ? /^([a-z0-9]+)\.\.([a-z0-9]+)$/.exec(value) : null;
  return m ? { from: m[1] as string, to: m[2] as string } : undefined;
}

export const compareParam = ({ from, to }: ComparePair) => `${from}..${to}`;

/**
 * The comparison Review shows: the one asked for, else the draft against what it started from
 * (its base in Definition, the template or clone in the wizard); none without a draft.
 */
export function reviewCompare(
  asked: string | undefined,
  { place, hasDraft }: { place: 'definition' | 'wizard'; hasDraft: boolean },
): ComparePair | undefined {
  const pair = parseCompare(asked);
  if (pair) return pair;
  if (!hasDraft) return undefined;
  return place === 'wizard' ? { from: 'source', to: 'draft' } : { from: 'base', to: 'draft' };
}

// The diff.

export interface DiffLine {
  kind: 'context' | 'added' | 'removed';
  text: string;
  /** The line's number in the new text; removed lines have none. */
  number?: number;
}

export interface Hunk {
  /** B's labels of the changes in this hunk ("Line: build station"); null when none match. */
  header: string | null;
  lines: DiffLine[];
}

const overlaps = ([a = 0, b = 0]: readonly number[], from: number, to: number) => a > 0 && a <= to && b >= from;

/** The name of a change's section ("Line", "Agents"); "Files" for one the editor does not know. */
export function sectionName(section: string): string {
  return section in sectionInfo ? sectionInfo[section as keyof typeof sectionInfo].label : 'Files';
}

/** "Line: build station", "Connections: browser", "Agents: reviewer". */
export function changeLabel(change: Change): string {
  const target = change.target.kind === 'station' ? `${change.target.id} station` : change.target.id;
  return `${sectionName(change.section)}: ${target}`;
}

/**
 * A unified diff of two texts, two lines of context, each hunk headed by the labels of B's
 * changes whose lines it touches (from_lines in the old text, to_lines in the new). The changes
 * carry lines of factory.yaml only, so other files' hunks go unlabelled.
 */
export function hunksOf(before: string, after: string, changes: readonly Change[] = []): Hunk[] {
  const patch = structuredPatch('a', 'b', before, after, '', '', { context: 2 });
  return patch.hunks.map((h) => {
    const lines: DiffLine[] = [];
    let n = h.newStart;
    for (const raw of h.lines) {
      const mark = raw[0];
      const text = raw.slice(1);
      if (mark === '\\') continue;
      if (mark === '-') lines.push({ kind: 'removed', text });
      else if (mark === '+') lines.push({ kind: 'added', text, number: n++ });
      else lines.push({ kind: 'context', text, number: n++ });
    }
    const oldEnd = h.oldStart + Math.max(h.oldLines - 1, 0);
    const newEnd = h.newStart + Math.max(h.newLines - 1, 0);
    const labels = changes
      .filter((c) => overlaps(c.to_lines, h.newStart, newEnd) || overlaps(c.from_lines, h.oldStart, oldEnd))
      .map(changeLabel);
    return { header: labels.length > 0 ? [...new Set(labels)].join(', ') : null, lines };
  });
}

// Publishing.

/** The version note a publish starts from: the first two change summaries, at most 200 characters. */
export function defaultMessage(changes: readonly Change[]): string {
  const text = changes
    .slice(0, 2)
    .map((c) => c.summary)
    .join('; ');
  return text.length > 200 ? `${text.slice(0, 199)}…` : text;
}

/** A station that open items wait at, which the new version removes, and where they may go. */
export interface MoveNeed {
  station: string;
  options: string[];
}

const moveTarget = (path: string) =>
  /^\/station_moves\/(.+)$/.exec(path)?.[1]?.replaceAll('~1', '/').replaceAll('~0', '~');

/** The station moves B's items check asks for: its publish fixes, one row per removed station. */
export function movesFromProblems(problems: readonly DefinitionProblem[]): MoveNeed[] {
  const out = new Map<string, Set<string>>();
  for (const p of problems) {
    for (const fix of p.fixes) {
      if (fix.section !== 'publish') continue;
      for (const op of fix.patch) {
        const station = moveTarget(op.path);
        if (!station || typeof op.value !== 'string') continue;
        out.set(station, (out.get(station) ?? new Set()).add(op.value));
      }
    }
  }
  return [...out].map(([station, options]) => ({ station, options: [...options] }));
}

/** Rows seen so far plus new ones: a row stays once its station is mapped (its problem is gone then). */
export function mergeMoveNeeds(known: readonly MoveNeed[], fresh: readonly MoveNeed[]): MoveNeed[] {
  const out = [...known];
  for (const need of fresh) {
    const i = out.findIndex((k) => k.station === need.station);
    if (i < 0) out.push(need);
    else out[i] = { station: need.station, options: [...new Set([...(out[i]?.options ?? []), ...need.options])] };
  }
  return out;
}

/** "Fix 1 problem first. Your draft stays saved." */
export function blockedSentence(errors: number): string {
  return `Fix ${plural(errors, 'problem')} first. Your draft stays saved.`;
}

/** What publishing does to the running version: the first publish saves v1; later, the roll-back line. */
export function publishExplainer(current: number | null, next: number): string {
  if (current === null) {
    return `Publishing saves version ${next}. Later edits start a new draft; nothing changes the running factory until you publish again.`;
  }
  return 'You can roll back to any earlier version later.';
}

// Versions.

const day = new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', timeZone: 'UTC' });

/** "Oct 21 by Local user". */
export function publishedLine(at: string | null, by: Actor | null): string {
  const when = at ? day.format(new Date(at)) : '';
  return by ? `${when} by ${by.name}`.trim() : when;
}

/** "All 21 versions". The list is one page, newest first, and numbers count up from 1, so the newest numbered version is the total. A draft has no number. */
export function allVersionsText(versions: readonly { number: number | null }[]): string {
  const newest = versions.find((v) => v.number !== null)?.number;
  return newest ? `All ${newest} versions` : 'All versions';
}

/** Where a draft or version started ("Template cost-review v2", "Clone of sre v3"). */
export function sourceText(source: VersionSource, templateTitle?: string): string {
  switch (source.kind) {
    case 'template': {
      const [name, version] = (source.template ?? '').split('@');
      return `Template ${templateTitle ?? name}${version ? ` v${version}` : ''}`;
    }
    case 'clone':
      return `Clone of ${source.factory ?? 'another factory'}${source.version ? ` v${source.version}` : ''}`;
    case 'import':
      return 'Imported YAML';
    case 'blank':
      return 'Blank';
    case 'cli':
      return 'Pushed from a working copy';
    case 'legacy':
      return 'Imported from the bucket';
    default:
      return source.version ? `Edit of v${source.version}` : 'Edit';
  }
}
