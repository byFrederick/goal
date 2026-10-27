// What the YAML view knows about the draft's files (D §10.9), as pure functions: the file tree
// (factory.yaml, folders with their file counts, the rest), each file's language, names a new file
// may take, binary content, and the lines a file adds or changes against the version the draft is
// based on (the change gutter).
import { diffLines } from 'diff';
import type { FileInfo } from '../../../api/types';
import { plural } from '../../../shared/format';

export const factoryFile = 'factory.yaml';

/** A file of the tree, named by its last segment. */
export interface TreeFile {
  kind: 'file';
  path: string;
  name: string;
}

/** A folder of the tree: its path, its name and how many files lie under it. */
export interface TreeFolder {
  kind: 'folder';
  path: string;
  name: string;
  count: number;
  children: TreeNode[];
}

export type TreeNode = TreeFile | TreeFolder;

/** The folders a definition always has, so New file… has a place even while they are empty. */
const alwaysFolders = ['agents', 'contracts'];

/**
 * The draft's files as a tree (the file tree of the YAML view): factory.yaml first, then the
 * folders by name, then the other files by name. agents/ and contracts/ are always there.
 */
export function fileTree(files: readonly FileInfo[]): TreeNode[] {
  const root: TreeFolder = { kind: 'folder', path: '', name: '', count: 0, children: [] };
  const folderIn = (parent: TreeFolder, name: string, path: string): TreeFolder => {
    const found = parent.children.find((c): c is TreeFolder => c.kind === 'folder' && c.name === name);
    if (found) return found;
    const made: TreeFolder = { kind: 'folder', path, name, count: 0, children: [] };
    parent.children.push(made);
    return made;
  };
  for (const name of alwaysFolders) folderIn(root, name, name);
  for (const f of files) {
    const parts = f.path.split('/');
    let folder = root;
    for (let i = 0; i < parts.length - 1; i++) {
      folder = folderIn(folder, parts[i] ?? '', parts.slice(0, i + 1).join('/'));
      folder.count += 1;
    }
    folder.children.push({ kind: 'file', path: f.path, name: parts.at(-1) ?? f.path });
  }
  return sortNodes(root.children);
}

/** factory.yaml, then folders, then files, each by name. */
function sortNodes(nodes: TreeNode[]): TreeNode[] {
  const rank = (n: TreeNode) => (n.kind === 'file' && n.path === factoryFile ? 0 : n.kind === 'folder' ? 1 : 2);
  const sorted = [...nodes].sort((a, b) => rank(a) - rank(b) || (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  for (const n of sorted) if (n.kind === 'folder') n.children = sortNodes(n.children);
  return sorted;
}

/** The folder a file lives in ("" at the root). */
export const folderOf = (path: string) => (path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '');

/** evals/ is edited from Jonah later: it is shown, not changed here. */
export const isReadOnlyPath = (path: string) => path === 'evals' || path.startsWith('evals/');

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
