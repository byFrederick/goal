// The YAML view's file tree (board Define-YAML, screens build/definition): "Find a file", the
// definition's folders with their file counts, a dot on each changed file and folder, and a count
// of errors on a file with problems. A folder opens and closes; the open file is marked
// aria-current. New file… sits on a folder row, Rename… and Delete… on the open file (not on
// factory.yaml, not in evals/, which is edited from Jonah later), and a view-only editor shows the
// files without them.
import { ChevronDown, ChevronRight, Ellipsis, File, Plus, Search } from 'lucide-react';
import { type ReactNode, useState } from 'react';
import { plural } from '../../../shared/format';
import { cn } from '../../../ui/cn';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../../ui/menu';
import { factoryFile, isReadOnlyPath, type TreeFolder, type TreeNode } from './files';

interface FileTreeProps {
  nodes: readonly TreeNode[];
  /** The open file. */
  path: string;
  /** Files that differ from the base version. */
  changed: ReadonlySet<string>;
  /** Errors per file (the problems of the saved draft or of the text being typed). */
  problems: ReadonlyMap<string, number>;
  /** The factory's name, the tree's heading. */
  factoryName: string;
  /** Files are shown, not added, renamed or deleted (a view-only editor). */
  readOnly?: boolean;
  onOpen: (path: string) => void;
  /** New file… in a folder ("" for the root). */
  onNew: (folder: string) => void;
  onRename: (path: string) => void;
  onDelete: (path: string) => void;
}

const rowBase =
  'flex min-h-7 min-w-0 flex-1 items-center gap-2 rounded-chip px-2 text-left text-secondary text-ink-soft hover:bg-hover hover:text-ink focus-visible:-outline-offset-2';

const hasInside = (folder: TreeFolder, path: string): boolean =>
  folder.children.some((c) => (c.kind === 'file' ? c.path === path : hasInside(c, path)));

const anyChanged = (folder: TreeFolder, changed: ReadonlySet<string>): boolean =>
  folder.children.some((c) => (c.kind === 'file' ? changed.has(c.path) : anyChanged(c, changed)));

/** A dot for a changed file or folder: a mark beside the name, the words for screen readers. */
function ChangedMark({
  on,
  className = 'ml-auto',
  words = true,
}: {
  on: boolean;
  className?: string;
  /** False when the words sit inside the name's own span, so the accessible name reads "name, changed" without a flex gap. */
  words?: boolean;
}) {
  return on ? (
    <>
      <span aria-hidden="true" className={cn(className, 'size-1.5 shrink-0 rounded-full bg-muted')} />
      {words ? <span className="sr-only">, changed</span> : null}
    </>
  ) : null;
}

/** The red count of a file's errors. */
function ErrorCount({ count }: { count: number }) {
  return count > 0 ? (
    <>
      <span
        aria-hidden="true"
        className="ml-auto rounded-chip bg-rework-tint px-1 text-meta font-control text-rework-ink"
      >
        {count}
      </span>
    </>
  ) : null;
}

export function FileTree({
  nodes,
  path,
  changed,
  problems,
  factoryName,
  readOnly = false,
  onOpen,
  onNew,
  onRename,
  onDelete,
}: FileTreeProps) {
  const [query, setQuery] = useState('');
  // A folder is open by default when it holds the open file or is agents/; a click flips that.
  const [flipped, setFlipped] = useState<ReadonlySet<string>>(() => new Set());
  const needle = query.trim().toLowerCase();

  const fileRow = (file: { path: string; name: string }): ReactNode => {
    const selected = file.path === path;
    const errors = problems.get(file.path) ?? 0;
    const menu = selected && !readOnly && file.path !== factoryFile && !isReadOnlyPath(file.path);
    return (
      <li key={file.path} className="flex items-center gap-1">
        <button
          type="button"
          aria-current={selected ? 'true' : undefined}
          // An sr-only span would be read with a space before the comma, so the name is set whole.
          aria-label={
            changed.has(file.path) || errors > 0
              ? `${file.name}${errors > 0 ? `, ${plural(errors, 'error')}` : ''}${changed.has(file.path) ? ', changed' : ''}`
              : undefined
          }
          onClick={() => onOpen(file.path)}
          className={cn(rowBase, selected && 'bg-hover font-control text-ink')}
        >
          <File size={14} strokeWidth={1.5} aria-hidden="true" className="shrink-0 text-muted" />
          <span className="truncate font-mono text-meta">{file.name}</span>
          <ErrorCount count={errors} />
          <ChangedMark on={changed.has(file.path)} words={false} />
        </button>
        {menu ? (
          <Menu>
            <MenuTrigger asChild>
              <button
                type="button"
                aria-label={`Actions for ${file.name}`}
                className="inline-flex size-8 shrink-0 items-center justify-center rounded-chip text-muted hover:bg-hover hover:text-ink"
              >
                <Ellipsis size={14} strokeWidth={1.75} aria-hidden="true" />
              </button>
            </MenuTrigger>
            <MenuContent side="bottom" className="w-60">
              <MenuItem onSelect={() => onRename(file.path)}>Rename {file.name}…</MenuItem>
              <MenuItem onSelect={() => onDelete(file.path)} className="text-rework-ink">
                Delete {file.name}…
              </MenuItem>
            </MenuContent>
          </Menu>
        ) : null}
      </li>
    );
  };

  const folderRow = (folder: TreeFolder): ReactNode => {
    const defaultOpen = folder.path === 'agents' || hasInside(folder, path);
    const open = defaultOpen !== flipped.has(folder.path);
    const canAdd = !readOnly && !isReadOnlyPath(folder.path);
    return (
      <li key={folder.path}>
        <div className="flex items-center gap-1">
          <button
            type="button"
            aria-expanded={open}
            onClick={() =>
              setFlipped((s) => {
                const next = new Set(s);
                if (next.has(folder.path)) next.delete(folder.path);
                else next.add(folder.path);
                return next;
              })
            }
            className={cn(rowBase, 'font-control text-ink')}
          >
            {open ? (
              <ChevronDown size={14} strokeWidth={1.75} aria-hidden="true" className="shrink-0 text-muted" />
            ) : (
              <ChevronRight size={14} strokeWidth={1.75} aria-hidden="true" className="shrink-0 text-muted" />
            )}
            <span className="truncate">{folder.name}</span>
            <span aria-hidden="true" className="ml-auto text-meta text-muted tabular-nums">
              {folder.count}
            </span>
            <span className="sr-only">, {plural(folder.count, 'file')}</span>
            <ChangedMark on={anyChanged(folder, changed)} className="" />
          </button>
          {canAdd ? (
            <button
              type="button"
              aria-label={`New file in ${folder.name}`}
              onClick={() => onNew(folder.path)}
              className="inline-flex size-8 shrink-0 items-center justify-center rounded-chip text-muted hover:bg-hover hover:text-ink"
            >
              <Plus size={14} strokeWidth={1.75} aria-hidden="true" />
            </button>
          ) : null}
        </div>
        {open ? <ul className="m-0 flex list-none flex-col gap-px p-0 pl-4">{children(folder.children)}</ul> : null}
      </li>
    );
  };

  const children = (list: readonly TreeNode[]): ReactNode[] =>
    list.map((node) => (node.kind === 'file' ? fileRow(node) : folderRow(node)));

  const matching = needle ? allFiles(nodes).filter((p) => p.toLowerCase().includes(needle)) : [];

  return (
    <nav aria-label="Definition files" className="flex min-w-0 flex-col bg-sidebar">
      <label className="m-2 flex h-8 items-center gap-2 rounded-control border border-rule bg-card px-3 text-muted focus-within:outline-2 focus-within:outline-run">
        <Search size={14} strokeWidth={1.75} aria-hidden="true" />
        <span className="sr-only">Find a file</span>
        <input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Find a file"
          className="min-w-0 flex-1 bg-transparent text-secondary text-ink outline-none placeholder:text-muted"
        />
      </label>
      <div className="flex items-center justify-between gap-2 px-3 py-1 text-meta font-control text-muted">
        <span className="truncate">{factoryName}</span>
        {readOnly ? null : (
          <button
            type="button"
            aria-label="New file in the definition"
            onClick={() => onNew('')}
            className="inline-flex size-11 md:size-6 shrink-0 items-center justify-center rounded-chip text-muted hover:bg-hover hover:text-ink"
          >
            <Plus size={14} strokeWidth={1.75} aria-hidden="true" />
          </button>
        )}
      </div>
      {needle ? (
        <ul aria-label="Matching files" className="m-0 flex list-none flex-col gap-px p-0 px-2">
          {matching.length === 0 ? (
            <li className="px-2 py-1 text-secondary text-muted">No file matches “{query.trim()}”.</li>
          ) : (
            matching.map((p) => fileRow({ path: p, name: p }))
          )}
        </ul>
      ) : (
        <ul className="m-0 flex list-none flex-col gap-px p-0 px-2">{children(nodes)}</ul>
      )}
      <p className="mt-auto p-3 text-meta text-muted">Stored by Goal, versioned on every publish</p>
    </nav>
  );
}

function allFiles(nodes: readonly TreeNode[]): string[] {
  return nodes.flatMap((n) => (n.kind === 'file' ? [n.path] : allFiles(n.children)));
}
