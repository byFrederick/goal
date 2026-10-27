// The YAML view of the whole draft (D §10.9, screens build/definition): the file tree beside the
// editor (Find a file, folders with counts, changed dots), the open file's tab with the Form |
// YAML toggle, the code editor (Monaco, the textarea on phones) with B's problems as markers and
// blocks under their lines, the change gutter against the base version, Format (B's encoder) and
// Copy, the Problems panel under the editor and the Outline column beside it. Typing saves the
// file through the draft session (800 ms) and, between saves, validates the unsaved text as an
// overlay (500 ms, latest wins) so problems follow the typing. The cursor's line is kept in
// `?line=`, so Form opens at the entity under it; a line asked for in the URL (a problem, Form's
// entity) is revealed. Without a draft it shows the running version, and the first keystroke opens
// a draft. Below 640 px of `main` the tree waits behind a Files button; below 1024 px the Outline
// waits.
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { useParams } from '@tanstack/react-router';
import { Copy, Download, WandSparkles } from 'lucide-react';
import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../../api/client';
import {
  compareVersions,
  deleteDraftFile,
  formatDefinition,
  getFileBlob,
  putDraftFile,
  validateDraft,
} from '../../../api/definitions';
import {
  defKeys,
  draftFileQuery,
  draftFilesQuery,
  draftQuery,
  factoryQuery,
  versionFileQuery,
  versionQuery,
} from '../../../api/queries';
import type { DefinitionProblem } from '../../../api/types';
import { cleanPath, lineOfPlace } from '../../../shared/definition/place';
import { useFactory } from '../../../shared/factory';
import { formatClock } from '../../../shared/format';
import { Button } from '../../../ui/button';
import { cn } from '../../../ui/cn';
import { useDraft, useDraftSession, useModel, useViewOnly } from '../draft/use-draft';
import { useValidation } from '../frame/use-editor';
import { ViewToggle } from '../frame/view-toggle';
import { useGoTo } from '../search-link';
import type { SectionViewProps } from '../section-views';
import { type EditorSection, isEditorSection } from '../sections';
import { CodeEditor, type CodeEditorHandle, type CursorPosition, type EditorMarker } from './code-editor';
import { type FileDialog, FileDialogs } from './file-dialogs';
import { FileTree } from './file-tree';
import {
  binarySentence,
  changedLines,
  factoryFile,
  fileTree,
  isBinary,
  languageNames,
  languageOf,
  newFileText,
} from './files';
import { outlineOf } from './outline';
import { Outline, ProblemBlock, ProblemsPanel } from './panels';

/** A value that settles `ms` after its last change. */
function useDebounced<T>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setSettled(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return settled;
}

/** How many lines Monaco shows before it scrolls inside: the window's height less the editor's frame. */
function useEditorLines(): number {
  const read = useCallback(() => Math.max(16, Math.floor((window.innerHeight - 180) / 23)), []);
  const [lines, setLines] = useState(read);
  useEffect(() => {
    const onResize = () => setLines(read());
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [read]);
  return lines;
}

/** The section and the entity of a route, the way the editor's routes name them. */
function useRouteSection(): EditorSection {
  const params = useParams({ strict: false }) as { section?: string; step?: string };
  const section = params.section ?? params.step ?? 'line';
  return isEditorSection(section) ? section : 'line';
}

const toMarker = (p: DefinitionProblem): EditorMarker => ({
  line: p.line,
  column: p.column,
  endLine: p.end_line || p.line,
  endColumn: p.end_column,
  severity: p.severity,
  message: p.message,
});

export function YamlView({ search }: SectionViewProps) {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const { org, factory: f } = session;
  const factory = useFactory();
  const client = useQueryClient();
  const go = useGoTo();
  const section = useRouteSection();
  const maxLines = useEditorLines();
  const panelId = useId();
  const treeId = useId();
  const problemsId = useId();
  const [filesOpen, setFilesOpen] = useState(false);

  const hasDraft = useDraft((s) => s.hasDraft);
  const revision = useDraft((s) => s.revision);
  const ranges = useDraft((s) => s.ranges);
  const parseError = useDraft((s) => s.parseError);
  const savedAt = useDraft((s) => s.savedAt);
  const model = useModel();
  const draft = useQuery({ ...draftQuery(org, f), enabled: hasDraft }).data;
  const current = useQuery(factoryQuery(org, f)).data?.current ?? null;
  const path = cleanPath(search.file ?? factoryFile);

  // The files: the draft's, or the running version's while no draft is open.
  const draftFiles = useQuery({ ...draftFilesQuery(org, f), enabled: hasDraft }).data;
  const currentDetail = useQuery({ ...versionQuery(org, f, 'current'), enabled: !hasDraft && current !== null }).data;
  const files = (hasDraft ? draftFiles : currentDetail?.files) ?? [];
  const tree = useMemo(() => fileTree(files), [files]);
  const info = files.find((x) => x.path === path);
  const fileState = useDraft((s) => s.files.get(path));
  const missing = fileState === undefined;

  const draftFile = useQuery({ ...draftFileQuery(org, f, path), enabled: hasDraft && info !== undefined });
  const versionFile = useQuery({
    ...versionFileQuery(org, f, 'current', path, info?.sha256 ?? ''),
    enabled: !hasDraft && info !== undefined,
  });
  const loaded = hasDraft ? draftFile.data : versionFile.data;
  // biome-ignore lint/correctness/useExhaustiveDependencies: `missing` re-adopts a file the session dropped (a publish)
  useEffect(() => {
    if (loaded) session.adoptFile(path, loaded);
  }, [loaded, path, session, missing]);

  const text = fileState?.text;
  const dirty = fileState?.dirty ?? false;
  const binary = text !== undefined && isBinary(text);
  const readOnly = viewOnly || path.startsWith('evals/') || binary;

  // Problems: of the saved draft, or of the text being typed (an overlay) between saves.
  const saved = useValidation();
  const typed = useDebounced(dirty ? text : undefined, 500);
  const overlay = useQuery({
    queryKey: [...defKeys.draft(org, f), 'overlay', revision, path, typed ?? ''],
    queryFn: ({ signal }) => validateDraft(org, f, { overlay: { [path]: { text: typed ?? '' } } }, signal),
    enabled: hasDraft && typed !== undefined,
    placeholderData: keepPreviousData,
  }).data;
  const validation = (dirty ? overlay : saved) ?? overlay ?? saved;
  const problems = validation?.problems ?? [];
  const here = problems.filter((p) => cleanPath(p.file ?? factoryFile) === path && p.line > 0);
  const unparseable =
    path === factoryFile && (here.some((p) => p.code === 'yaml-invalid') || (!dirty && parseError !== null));
  // The file tree's marks: errors per file, and the files that differ from the base version.
  const errorsByFile = useMemo(() => {
    const counts = new Map<string, number>();
    for (const p of problems) {
      if (p.severity === 'info') continue;
      const file = cleanPath(p.file ?? factoryFile);
      counts.set(file, (counts.get(file) ?? 0) + 1);
    }
    return counts;
  }, [problems]);

  // The change gutter: this file against the version the draft is based on.
  const comparison = useQuery({
    queryKey: defKeys.compare(org, f, 'base', 'draft', revision),
    queryFn: ({ signal }) => compareVersions(org, f, 'base', 'draft', signal),
    enabled: hasDraft,
    placeholderData: keepPreviousData,
  }).data;
  const changedFiles = useMemo(
    () => new Set((hasDraft ? (comparison?.files ?? []) : []).filter((x) => x.op !== 'unchanged').map((x) => x.path)),
    [comparison, hasDraft],
  );
  const fileChange = hasDraft ? comparison?.files.find((x) => x.path === path) : undefined;
  const baseText = useQuery({
    ...versionFileQuery(org, f, 'base', path, fileChange?.from_sha256 ?? ''),
    enabled: Boolean(fileChange?.from_sha256),
  }).data?.text;
  const changed = useMemo(() => {
    if (!fileChange || text === undefined || fileChange.op === 'unchanged') return [];
    if (fileChange.op === 'added') return changedLines(null, text);
    return baseText === undefined ? [] : changedLines(baseText, text);
  }, [fileChange, text, baseText]);

  // The Outline: the keys of the open YAML file, with the lines that have problems.
  const outline = useMemo(() => {
    if (text === undefined || languageOf(path) !== 'yaml') return [];
    const problemLines = problems
      .filter((p) => p.severity !== 'info' && p.line > 0 && cleanPath(p.file ?? factoryFile) === path)
      .map((p) => p.line);
    return outlineOf(text, problemLines);
  }, [text, path, problems]);

  // Keeping the place: the cursor's line goes to ?line=; a line asked for elsewhere is revealed.
  const [editor, setEditor] = useState<CodeEditorHandle | null>(null);
  const [cursor, setCursor] = useState<CursorPosition | null>(null);
  const written = useRef<number | undefined>(undefined);
  const revealed = useRef('');
  const { mode: _m, file: _f, line: _l, compare: _c, add: _a, ...entity } = search;
  const entryLine =
    search.line ?? (path === factoryFile ? lineOfPlace(model, ranges, { section, ...entity } as never) : undefined);
  // biome-ignore lint/correctness/useExhaustiveDependencies: revealed once per file and asked-for line
  useEffect(() => {
    if (!editor || text === undefined) return;
    const key = `${path}:${search.line ?? 'entry'}`;
    if (revealed.current === key) return;
    if (search.line !== undefined && search.line === written.current) {
      revealed.current = key;
      return;
    }
    if (entryLine === undefined || editor.reveal(entryLine)) revealed.current = key;
  }, [editor, path, search.line, entryLine, text === undefined]);

  const cursorLine = cursor?.line;
  // biome-ignore lint/correctness/useExhaustiveDependencies: only the cursor's moves write the URL
  useEffect(() => {
    if (cursorLine === undefined) return;
    const t = setTimeout(() => {
      if (cursorLine === search.line) return;
      written.current = cursorLine;
      go(undefined, { ...search, line: cursorLine }, true);
    }, 400);
    return () => clearTimeout(t);
  }, [cursorLine]);

  const revealProblem = (p: DefinitionProblem) => {
    const file = cleanPath(p.file ?? factoryFile);
    if (file !== path) go(undefined, { mode: 'yaml', ...(file !== factoryFile ? { file } : {}), line: p.line });
    else editor?.reveal(p.line, true);
  };

  // Zones: under each line with problems, its problems (Monaco only).
  const byLine = new Map<number, DefinitionProblem[]>();
  for (const p of here) {
    if (p.severity === 'info') continue;
    const line = p.end_line || p.line;
    byLine.set(line, [...(byLine.get(line) ?? []), p]);
  }
  const zones = [...byLine].map(([line, list]) => ({
    key: `problems-${line}`,
    line,
    content: <ProblemBlock problems={list} />,
  }));

  // Files.
  const [dialog, setDialog] = useState<FileDialog>(null);
  const refresh = () => client.invalidateQueries({ queryKey: defKeys.draft(org, f) });
  const open = (file: string) => {
    setFilesOpen(false);
    go(undefined, { mode: 'yaml', ...(file !== factoryFile ? { file } : {}) });
  };
  const create = (file: string) => {
    session.editFile(file, newFileText(file));
    open(file);
  };
  const remove = async (file: string) => {
    await session.flush();
    const sha = session.getSnapshot().files.get(file)?.serverSha ?? files.find((x) => x.path === file)?.sha256;
    if (sha) await deleteDraftFile(org, f, file, sha);
    session.forgetFile(file);
    await refresh();
    open(factoryFile);
    toast(`Deleted ${file}`);
  };
  const rename = async (from: string, to: string) => {
    await session.flush();
    const s = session.getSnapshot().files.get(from);
    await putDraftFile(org, f, to, s?.text ?? '', { create: true });
    const sha = s?.serverSha ?? files.find((x) => x.path === from)?.sha256;
    if (sha) await deleteDraftFile(org, f, from, sha);
    session.forgetFile(from);
    await refresh();
    open(to);
    toast(`Renamed to ${to}`);
  };

  const format = async () => {
    if (text === undefined) return;
    try {
      const out = await formatDefinition(org, text);
      if (out !== text) session.editFile(path, out);
      toast(out === text ? 'factory.yaml is already formatted' : 'Formatted factory.yaml');
    } catch (err) {
      toast.error(`Not formatted: ${err instanceof ApiError ? (err.detail ?? err.title) : 'goald can’t be reached'}`);
    }
  };
  const copy = async () => {
    if (text === undefined) return;
    try {
      await navigator.clipboard.writeText(text);
      toast(`Copied ${path}`);
    } catch {
      toast.error('The browser did not allow copying');
    }
  };

  const version = hasDraft
    ? draft && (draft.base || current)
      ? `draft of v${draft.next_number}`
      : 'draft'
    : current
      ? `v${current.number}`
      : 'draft';
  const label = `${path}, ${version}`;
  const language = languageOf(path);
  const saving = dirty || fileState?.inFlight;
  const savedLine = !hasDraft
    ? 'Your first change opens a draft.'
    : saving
      ? 'Saving…'
      : (savedAt ?? draft?.updated_at)
        ? `Autosaved ${formatClock((savedAt ?? draft?.updated_at) as string)}.`
        : '';
  const formatHint = useId();

  return (
    <>
      <h2 className="sr-only" data-route-focus="" tabIndex={-1}>
        YAML of the {version}
      </h2>
      <div className="grid min-h-180 min-w-0 overflow-hidden rounded-card border border-line @[640px]:grid-cols-[250px_minmax(0,1fr)] @[1024px]:grid-cols-[250px_minmax(0,1fr)_260px]">
        <div id={treeId} className={cn(filesOpen ? 'flex' : 'hidden', '@[640px]:flex', 'min-w-0 border-r border-line')}>
          <FileTree
            nodes={tree}
            path={path}
            changed={changedFiles}
            problems={errorsByFile}
            factoryName={factory.name}
            readOnly={viewOnly}
            onOpen={open}
            onNew={(folder) => setDialog({ kind: 'new', folder })}
            onRename={(p) => setDialog({ kind: 'rename', path: p })}
            onDelete={(p) => setDialog({ kind: 'delete', path: p })}
          />
        </div>

        <div className="flex min-w-0 flex-col">
          <div className="flex flex-wrap items-stretch justify-between gap-x-3 gap-y-2 border-b border-line bg-panel">
            <div className="flex min-w-0 items-stretch">
              <button
                type="button"
                aria-expanded={filesOpen}
                aria-controls={treeId}
                onClick={() => setFilesOpen((v) => !v)}
                className="inline-flex items-center px-3 text-secondary font-control text-ink @[640px]:hidden"
              >
                Files
              </button>
              <div role="tablist" aria-label="Open files" className="flex min-w-0">
                <button
                  type="button"
                  role="tab"
                  aria-selected="true"
                  aria-controls={panelId}
                  tabIndex={0}
                  className="inline-flex h-10 min-w-0 items-center gap-2 border-t-2 border-run bg-shell px-4 font-mono text-secondary text-ink"
                >
                  <span className="truncate">{path}</span>
                </button>
              </div>
            </div>
            <div className="flex items-center px-3 py-1">
              <ViewToggle section={section} yaml />
            </div>
          </div>

          {unparseable ? (
            <p
              id={formatHint}
              className="m-0 border-b border-line bg-rework-tint px-4 py-2 text-secondary text-rework-ink"
            >
              factory.yaml doesn’t parse{parseError?.line ? ` at line ${parseError.line}` : ''}: Format and Form wait
              until it does.
            </p>
          ) : null}

          <div id={panelId} role="tabpanel" aria-label={label} className="min-w-0">
            {text === undefined ? (
              info === undefined && files.length > 0 ? (
                <p className="m-0 p-5 text-secondary text-muted">
                  {path} is not in the draft.{' '}
                  <button
                    type="button"
                    onClick={() => open(factoryFile)}
                    className="text-ink underline underline-offset-3"
                  >
                    Open factory.yaml
                  </button>
                </p>
              ) : (
                <div aria-busy="true" className="h-64">
                  <span className="sr-only">Loading {path}</span>
                </div>
              )
            ) : binary ? (
              <div className="flex flex-wrap items-center justify-between gap-3 p-5 text-secondary">
                <span>{binarySentence(info?.size ?? text.length)}</span>
                <Button
                  size="sm"
                  variant="white"
                  className="h-11 md:h-8"
                  onClick={() => void download(org, f, hasDraft ? 'draft' : 'current', path)}
                >
                  <Download size={14} strokeWidth={1.75} aria-hidden="true" />
                  Download
                </Button>
              </div>
            ) : (
              <CodeEditor
                ref={setEditor}
                path={path}
                value={text}
                label={label}
                language={language}
                onChange={(v: string) => session.editFile(path, v)}
                onBlur={() => void session.flush()}
                readOnly={readOnly}
                minLines={12}
                maxLines={maxLines}
                markers={here.map(toMarker)}
                changedLines={changed}
                zones={zones}
                onCursor={setCursor}
              />
            )}
          </div>

          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t border-line px-4 py-2 text-meta text-muted">
            <span>
              Line {cursor?.line ?? 1}, column {cursor?.column ?? 1}. {languageNames[language]}
              {path === factoryFile ? ', schema goal/v1' : ''}
              {viewOnly ? '. View only' : readOnly && !binary ? '. Read-only: edited from Jonah later' : ''}
            </span>
            <span className="flex flex-wrap items-center gap-2">
              {path === factoryFile && !viewOnly ? (
                <Button
                  size="sm"
                  variant="white"
                  className="h-11 md:h-7"
                  onClick={() => void format()}
                  disabled={unparseable || text === undefined}
                  aria-describedby={unparseable ? formatHint : undefined}
                >
                  <WandSparkles size={14} strokeWidth={1.75} aria-hidden="true" />
                  Format
                </Button>
              ) : null}
              {binary ? null : (
                <Button
                  size="sm"
                  variant="white"
                  className="h-11 md:h-7"
                  onClick={() => void copy()}
                  disabled={text === undefined}
                >
                  <Copy size={14} strokeWidth={1.75} aria-hidden="true" />
                  Copy
                </Button>
              )}
              <span role="status">
                {path === factoryFile ? 'Checked as you type. ' : ''}
                {savedLine}
              </span>
            </span>
          </div>

          <ProblemsPanel problems={problems} onGo={revealProblem} headingId={problemsId} />
        </div>

        <Outline items={outline} onGo={(line) => editor?.reveal(line, true)} />
      </div>

      <FileDialogs
        dialog={dialog}
        onClose={() => setDialog(null)}
        taken={new Set(files.map((x) => x.path))}
        onCreate={create}
        onRename={rename}
        onDelete={remove}
      />
    </>
  );
}

/**
 * Saves a file of the draft or the running version: its bytes come through the API client (goald
 * may be on another origin, where a link's `download` is ignored), then a link to a blob URL.
 */
async function download(org: string, factory: string, version: string, path: string) {
  try {
    const url = URL.createObjectURL(await getFileBlob(org, factory, version, path));
    const link = document.createElement('a');
    link.href = url;
    link.download = path.split('/').at(-1) ?? path;
    link.click();
    // After the click has started the download.
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  } catch {
    toast.error(`${path} couldn’t be downloaded. Try again.`);
  }
}
