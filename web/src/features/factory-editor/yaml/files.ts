// What the YAML view knows about the draft's files (D §10.9), as pure functions: the file tabs'
// groups (factory.yaml, agents/, one per local skill folder, contracts/, evals/, other files),
// each file's language, names a new file may take, binary content, and the lines a file adds or
// changes against the version the draft is based on (the change gutter).
import { diffLines } from 'diff';
import type { FileInfo } from '../../../api/types';
import { plural } from '../../../shared/format';

export const factoryFile = 'factory.yaml';

export interface FileGroup {
  /** Stable key: the folder, or factory.yaml, or "" for files at the root. */
  id: string;
  /** "agents/ (6)", "skills/acme-conventions", "contracts/ (2)". */
  label: string;
  /** Where New file… creates files; null for factory.yaml. */
  folder: string | null;
  files: FileInfo[];
  /** evals/ is edited from Jonah later. */
  readOnly: boolean;
}

const sortByPath = (a: FileInfo, b: FileInfo) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0);

/** The tabs over a draft's files, in the order of the board: factory.yaml, agents, skills, contracts, evals, the rest. */
export function fileGroups(files: readonly FileInfo[]): FileGroup[] {
  const sorted = [...files].sort(sortByPath);
  const under = (folder: string) => sorted.filter((f) => f.path.startsWith(`${folder}/`));
  const groups: FileGroup[] = [
    {
      id: factoryFile,
      label: factoryFile,
      folder: null,
      files: sorted.filter((f) => f.path === factoryFile),
      readOnly: false,
    },
  ];
  const agents = under('agents');
  groups.push({ id: 'agents', label: `agents/ (${agents.length})`, folder: 'agents', files: agents, readOnly: false });

  const skillFolders = [
    ...new Set(
      under('skills')
        .map((f) => f.path.split('/')[1])
        .filter((s): s is string => Boolean(s)),
    ),
  ];
  for (const skill of skillFolders) {
    const folder = `skills/${skill}`;
    groups.push({ id: folder, label: folder, folder, files: under(folder), readOnly: false });
  }

  const contracts = under('contracts');
  groups.push({
    id: 'contracts',
    label: `contracts/ (${contracts.length})`,
    folder: 'contracts',
    files: contracts,
    readOnly: false,
  });
  const evals = under('evals');
  if (evals.length > 0) {
    groups.push({ id: 'evals', label: `evals/ (${evals.length})`, folder: 'evals', files: evals, readOnly: true });
  }
  const known = new Set(groups.flatMap((g) => g.files.map((f) => f.path)));
  const rest = sorted.filter((f) => !known.has(f.path) && !f.path.startsWith('skills/'));
  if (rest.length > 0) {
    groups.push({ id: '', label: `other files (${rest.length})`, folder: '', files: rest, readOnly: false });
  }
  return groups;
}

/** The group a path belongs to (by folder), for a file the list does not have yet (a new file). */
export function groupOf(groups: readonly FileGroup[], path: string): FileGroup | undefined {
  if (path === factoryFile) return groups[0];
  const byFile = groups.find((g) => g.files.some((f) => f.path === path));
  if (byFile) return byFile;
  return groups.find((g) => g.folder && path.startsWith(`${g.folder}/`)) ?? groups.find((g) => g.folder === '');
}

export type Language = 'yaml' | 'markdown' | 'plaintext';

export function languageOf(path: string): Language {
  if (/\.ya?ml$/i.test(path)) return 'yaml';
  if (/\.md$/i.test(path)) return 'markdown';
  return 'plaintext';
}

/** How the status bar names a language. */
export const languageNames: Record<Language, string> = { yaml: 'YAML', markdown: 'Markdown', plaintext: 'Plain text' };

/** A file name New file… and Rename accept: no folders, no hidden files, no "..". */
export function fileNameProblem(name: string): string | undefined {
  if (name === '') return 'Name the file.';
  if (name.includes('/') || name.includes('\\')) return 'A name, not a path: the file goes in this folder.';
  if (name.startsWith('.')) return 'Hidden files are not part of a definition.';
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(name)) return 'Letters, digits, dots, dashes and underscores only.';
  return undefined;
}

/** What a new file starts with: a Markdown file its title. */
export function newFileText(path: string): string {
  const name = path.split('/').at(-1) ?? path;
  return languageOf(path) === 'markdown' ? `# ${name.replace(/\.md$/i, '')}\n` : '';
}

/** A file whose bytes are not text (they hold a NUL): shown as "Binary file, N KB" with Download. */
export const isBinary = (text: string) => text.includes('\u0000');

/** "Binary file, 12 KB". */
export function binarySentence(size: number): string {
  return `Binary file, ${size < 1024 ? `${size} bytes` : `${Math.round(size / 1024)} KB`}`;
}

/** The lines of `text` (1-based) that are new or changed against `base`; every line when base is null. */
export function changedLines(base: string | null, text: string): number[] {
  if (base === null) return text === '' ? [] : Array.from({ length: lineCount(text) }, (_, i) => i + 1);
  const out: number[] = [];
  let line = 1;
  for (const part of diffLines(base, text)) {
    const n = part.count ?? 0;
    if (part.removed) continue;
    if (part.added) for (let i = 0; i < n; i++) out.push(line + i);
    line += n;
  }
  return out;
}

/** The lines of a text, not counting the empty one after a final newline. */
export function lineCount(text: string): number {
  const n = text.split('\n').length;
  return text.endsWith('\n') ? n - 1 : n;
}

/** "Problems 2" in the panel's heading, for screen readers "2 problems". */
export const problemsLabel = (n: number) => (n === 0 ? 'No problems' : plural(n, 'problem'));
