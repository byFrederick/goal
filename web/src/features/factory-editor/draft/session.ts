// The draft session (D §10.4): the client-side model of one open draft, one instance per mounted
// editor (provided by use-draft.ts, read through useSyncExternalStore). It keeps each section's
// last server value and etag beside the local value, so edits are optimistic and survive
// switching sections; it autosaves each section and file 800 ms after its last edit (or at once
// on flush), one request in flight per section or file; it merges a 412 three ways (disjoint
// changes are sent again, overlapping leaves wait for the conflict dialog); it backs off while
// goald cannot be reached; and it holds Visual saves while factory.yaml does not parse. Without
// an open draft it shows the running version, and the first save opens a draft.
//
// It is not a store library: one class with a snapshot, replaced on every change.
import { ApiError } from '../../../api/client';
import * as definitions from '../../../api/definitions';
import type {
  DraftModel,
  FileMap,
  ParseError,
  Ranges,
  SaveResult,
  SectionSave,
  SectionSummary,
  VersionModel,
} from '../../../api/types';
import { type Model, modelOf, type Section, type SectionValues, sections } from '../../../shared/definition/model';
import { type Merged, merge3, resolve } from './merge';
import type { AnyEdit } from './ops';
import { equal } from './pointer';

/** The draft routes the session calls (definitions.ts; tests pass a fake of goald). */
export type SessionApi = Pick<
  typeof definitions,
  'getDraftModel' | 'openDraft' | 'saveDraftSection' | 'revertDraftSection' | 'putDraftFile' | 'discardDraft'
>;

export interface SessionOptions {
  org: string;
  /** The factory's id. */
  factory: string;
  api?: SessionApi;
  /** Called once a burst of saves has settled: refetch the draft's queries (draft, model, files). */
  refresh?: () => void;
  /** Autosave delay after the last edit of a section or file; 800 ms (B's figure). */
  debounceMs?: number;
}

/**
 * - saved: nothing waits; saving: an edit waits for its debounce or is in flight;
 * - offline: goald can't be reached, retrying with backoff; retrying: goald failed (5xx), same;
 * - conflict: a section or file changed elsewhere in the same place, the dialog decides;
 * - held: factory.yaml does not parse, Visual saves wait until it does;
 * - failed: goald refused a save for another reason (`error`); it is not retried.
 */
export type SaveStatus = 'saved' | 'saving' | 'offline' | 'retrying' | 'conflict' | 'held' | 'failed';

export interface SectionState<S extends Section = Section> {
  /** The value goald last answered for the section: the base of a merge. */
  server: SectionValues[S];
  serverEtag: string;
  /** What the editor shows: server plus unsaved edits. */
  local: SectionValues[S];
  dirty: boolean;
  inFlight: boolean;
}

export type SectionStates = { [S in Section]: SectionState<S> };

export interface FileState {
  /** The sha256 goald has; null for a file the draft does not have yet. */
  serverSha: string | null;
  /** The text goald has; null for a new file. */
  server: string | null;
  text: string;
  dirty: boolean;
  inFlight: boolean;
}

/** A section changed elsewhere where it changed here too: the conflict dialog's rows. */
export interface SectionConflict<S extends Section = Section> {
  section: S;
  merged: Merged<SectionValues[S]>;
  theirs: SectionValues[S];
  theirsEtag: string;
}

/** A file changed elsewhere: their version beside yours. */
export interface FileConflict {
  path: string;
  /** null: the file is gone. */
  theirsSha: string | null;
  ours: string;
}

export interface SessionState {
  /** A model was adopted: sections hold real values. */
  ready: boolean;
  /** A draft is open; without one the sections show the running version. */
  hasDraft: boolean;
  revision: number;
  /** The highest revision this session saved of the open draft (its stream echoes). */
  lastSaved: number;
  sections: SectionStates;
  files: ReadonlyMap<string, FileState>;
  /** B's edited pointers (Edited · Undo), ranges, section tiles and counts, as last answered. */
  edited: Record<string, unknown>;
  ranges: Ranges;
  summaries: SectionSummary[];
  changeCount: number;
  problemCount: number;
  status: SaveStatus;
  /** The last save's time ("Autosaved 14:32"). */
  savedAt: string | null;
  /** When the next retry runs (epoch ms), while offline or retrying. */
  retryAt: number | null;
  /** goald's refusal while failed. */
  error: ApiError | null;
  conflicts: { [S in Section]?: SectionConflict<S> };
  fileConflicts: Readonly<Record<string, FileConflict>>;
  parseError: ParseError | null;
  /** When a save made elsewhere (another tab, the CLI) arrived ("Updated elsewhere at 14:35"). */
  updatedElsewhereAt: string | null;
  /** The draft was discarded elsewhere ("Start a new draft" / "Leave"). */
  discarded: boolean;
  canUndo: boolean;
  canRedo: boolean;
}

interface UndoEntry {
  changes: { section: Section; before: unknown; after: unknown }[];
  /** Typing into one field: the next edit with the same key extends this step. */
  coalesce?: string;
}

export interface EditOptions {
  /** false: not an undo step (a value goald put back). */
  undoable?: boolean;
  /**
   * Typing: consecutive edits with the same key (a field's pointer) are one undo step, so
   * Ctrl/Cmd+Z outside the field puts back what was there before the typing started.
   */
  coalesce?: string;
}

const backoff = [1000, 2000, 4000, 8000, 16000, 30000];
const undoDepth = 100;

const emptyModel = (): Model => ({ basics: {}, line: {}, agents: {}, connections: [], review: {} });

function emptySections(m: Model = emptyModel(), etags?: Record<Section, string>): SectionStates {
  const out = {} as Record<Section, SectionState>;
  for (const s of sections) {
    out[s] = { server: m[s], serverEtag: etags?.[s] ?? '', local: m[s], dirty: false, inFlight: false };
  }
  return out as SectionStates;
}

type Key = Section | `file:${string}`;
const fileKey = (path: string): Key => `file:${path}`;

/** The hex sha256 of a text, as goald names files. */
async function sha256(text: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text));
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
}

export class DraftSession {
  private state: SessionState;
  private readonly listeners = new Set<() => void>();
  private readonly api: SessionApi;
  private readonly debounceMs: number;
  private readonly timers = new Map<Key, ReturnType<typeof setTimeout>>();
  private readonly inflight = new Map<Key, Promise<void>>();
  /** Files and removals edits attached to a section, sent with its next save. */
  private readonly pendingFiles = new Map<Section, { files: FileMap; remove: Set<string> }>();
  /** Keys whose last save failed and wait for the retry timer. */
  private readonly failed = new Set<Key>();
  private retryTimer: ReturnType<typeof setTimeout> | undefined;
  /** Why saves wait for the retry timer: goald could not be reached, or it failed. */
  private retryKind: 'offline' | 'retrying' = 'offline';
  private attempt = 0;
  private opening: Promise<void> | undefined;
  private savedSinceRefresh = false;
  private undoStack: UndoEntry[] = [];
  private redoStack: UndoEntry[] = [];
  private disposed = false;

  constructor(private readonly opts: SessionOptions) {
    this.api = opts.api ?? definitions;
    this.debounceMs = opts.debounceMs ?? 800;
    this.state = {
      ready: false,
      hasDraft: false,
      revision: 0,
      lastSaved: 0,
      sections: emptySections(),
      files: new Map(),
      edited: {},
      ranges: {},
      summaries: [],
      changeCount: 0,
      problemCount: 0,
      status: 'saved',
      savedAt: null,
      retryAt: null,
      error: null,
      conflicts: {},
      fileConflicts: {},
      parseError: null,
      updatedElsewhereAt: null,
      discarded: false,
      canUndo: false,
      canRedo: false,
    };
  }

  /** The organization's id. */
  get org(): string {
    return this.opts.org;
  }

  /** The factory's id. */
  get factory(): string {
    return this.opts.factory;
  }

  // The store.

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getSnapshot = (): SessionState => this.state;

  private set(patch: Partial<SessionState>): void {
    if (this.disposed) return;
    const next = { ...this.state, ...patch };
    next.status = this.statusOf(next);
    next.canUndo = this.undoStack.length > 0;
    next.canRedo = this.redoStack.length > 0;
    this.state = next;
    for (const l of this.listeners) l();
  }

  private statusOf(s: SessionState): SaveStatus {
    if (Object.keys(s.conflicts).length > 0 || Object.keys(s.fileConflicts).length > 0) return 'conflict';
    if (s.parseError) return 'held';
    if (s.error) return 'failed';
    if (s.retryAt !== null) return this.retryKind;
    const busy =
      sections.some((k) => s.sections[k].dirty || s.sections[k].inFlight) ||
      [...s.files.values()].some((f) => f.dirty || f.inFlight);
    return busy ? 'saving' : 'saved';
  }

  private setSection<S extends Section>(section: S, patch: Partial<SectionState<S>>): void {
    const sectionsNext = { ...this.state.sections, [section]: { ...this.state.sections[section], ...patch } };
    this.set({ sections: sectionsNext as SectionStates });
  }

  private setFile(path: string, patch: Partial<FileState>): void {
    const files = new Map(this.state.files);
    const cur = files.get(path) ?? { serverSha: null, server: null, text: '', dirty: false, inFlight: false };
    files.set(path, { ...cur, ...patch });
    this.set({ files });
  }

  // What goald answered.

  /** A draft model goald answered (the first load, or a refetch): clean sections take its values. */
  adopt(model: DraftModel): void {
    const m = modelOf(model.model);
    const before = this.state;
    const next = { ...before.sections } as Record<Section, SectionState>;
    for (const s of sections) {
      const cur = before.sections[s];
      if (cur.dirty || cur.inFlight) continue;
      next[s] = { server: m[s], serverEtag: model.etags[s], local: m[s], dirty: false, inFlight: false };
    }
    const newer = model.revision >= before.revision;
    const elsewhere =
      before.ready && before.hasDraft && model.revision > before.revision && model.revision > before.lastSaved;
    this.set({
      ready: true,
      hasDraft: true,
      discarded: false,
      sections: next as SectionStates,
      parseError: null,
      ...(newer ? { revision: model.revision, edited: model.edited, ranges: model.ranges } : {}),
      ...(elsewhere ? { updatedElsewhereAt: new Date().toISOString() } : {}),
    });
    if (before.parseError) this.scheduleDirty();
  }

  /** No draft is open: the editor shows this version (the running one) until the first edit. */
  adoptVersion(version: VersionModel): void {
    const m = modelOf(version.model);
    const next = { ...this.state.sections } as Record<Section, SectionState>;
    for (const s of sections) {
      const cur = this.state.sections[s];
      if (cur.dirty || cur.inFlight) continue;
      next[s] = { server: m[s], serverEtag: '', local: m[s], dirty: false, inFlight: false };
    }
    this.set({
      ready: true,
      hasDraft: false,
      sections: next as SectionStates,
      ranges: version.ranges,
      edited: {},
      revision: 0,
      lastSaved: 0,
    });
  }

  /** The draft is gone (404 no-draft): discarded elsewhere when this session had it open. */
  adoptMissing(): void {
    if (this.state.hasDraft) this.set({ discarded: true, hasDraft: false });
  }

  /** factory.yaml does not parse (409 yaml-invalid): Visual is read-only and saves wait. */
  adoptParseError(error: ParseError): void {
    this.set({ parseError: error, ready: true, hasDraft: true });
  }

  /** A file of the draft as loaded; unsaved text of it is kept. */
  adoptFile(path: string, file: { text: string; sha256: string }): void {
    const cur = this.state.files.get(path);
    if (cur?.inFlight) return;
    if (cur?.dirty) {
      this.setFile(path, { server: file.text, serverSha: cur.serverSha ?? file.sha256 });
      return;
    }
    this.setFile(path, { server: file.text, serverSha: file.sha256, text: file.text, dirty: false });
  }

  /** "Start a new draft" after a discard elsewhere: the next save opens one, unsaved edits kept. */
  restart(): void {
    this.set({ discarded: false, hasDraft: false });
    this.scheduleDirty();
  }

  // Edits.

  /** Replaces sections' local values (ops.ts), queues their saves, and records one undo step. */
  edit(edits: AnyEdit | AnyEdit[], { undoable = true, coalesce }: EditOptions = {}): void {
    const list = Array.isArray(edits) ? edits : [edits];
    const changes: UndoEntry['changes'] = [];
    for (const e of list) {
      const cur = this.state.sections[e.section];
      changes.push({ section: e.section, before: cur.local, after: e.value });
      if (e.files || e.removeFiles) {
        const pending = this.pendingFiles.get(e.section) ?? { files: {}, remove: new Set<string>() };
        for (const [p, c] of Object.entries(e.files ?? {})) {
          pending.files[p] = c;
          pending.remove.delete(p);
        }
        for (const p of e.removeFiles ?? []) {
          delete pending.files[p];
          pending.remove.add(p);
        }
        this.pendingFiles.set(e.section, pending);
      }
      this.setLocal(e.section, e.value);
    }
    if (undoable && changes.length > 0) {
      const last = this.undoStack.at(-1);
      if (coalesce !== undefined && last?.coalesce === coalesce && this.redoStack.length === 0) {
        last.changes = last.changes.map((c) => ({
          ...c,
          after: changes.find((n) => n.section === c.section)?.after ?? c.after,
        }));
        this.set({});
        return;
      }
      this.undoStack = [...this.undoStack.slice(-(undoDepth - 1)), { changes, ...(coalesce ? { coalesce } : {}) }];
      this.redoStack = [];
      this.set({});
    }
  }

  private setLocal<S extends Section>(section: S, value: SectionValues[S]): void {
    const cur = this.state.sections[section];
    const dirty = !equal(value, cur.server) || this.pendingFiles.has(section);
    this.setSection(section, { local: value, dirty });
    if (dirty) this.schedule(section);
  }

  /** Types into a file (YAML view, a prompt): saved as typed, 800 ms after the last keystroke. */
  editFile(path: string, text: string): void {
    const cur = this.state.files.get(path);
    const dirty = text !== (cur?.server ?? null);
    this.setFile(path, { text, dirty });
    if (dirty) this.schedule(fileKey(path));
  }

  /** Ctrl/Cmd+Z: puts the sections back as they were before the last edit, as a new save. */
  undo(): boolean {
    const entry = this.undoStack.pop();
    if (!entry) return false;
    const below = this.undoStack.at(-1);
    if (below) delete below.coalesce;
    this.redoStack.push(entry);
    for (const c of [...entry.changes].reverse()) this.setLocal(c.section, c.before as SectionValues[Section]);
    this.set({});
    return true;
  }

  redo(): boolean {
    const entry = this.redoStack.pop();
    if (!entry) return false;
    this.undoStack.push(entry);
    for (const c of entry.changes) this.setLocal(c.section, c.after as SectionValues[Section]);
    this.set({});
    return true;
  }

  // Conflicts.

  /** Decides a section's conflict: theirs where `keep` says so, mine elsewhere; then saves it. */
  resolveSection(section: Section, keep: Readonly<Record<string, 'theirs' | 'ours'>>): void {
    const c = this.state.conflicts[section];
    if (!c) return;
    const value = resolve(c.merged as Merged<unknown>, keep) as SectionValues[Section];
    const { [section]: _done, ...rest } = this.state.conflicts;
    this.set({ conflicts: rest });
    this.setSection(section, { server: c.theirs, serverEtag: c.theirsEtag });
    this.setLocal(section, value);
  }

  /** Keep theirs on every conflicting field of the section. */
  keepTheirs(section: Section): void {
    const c = this.state.conflicts[section];
    if (c)
      this.resolveSection(section, Object.fromEntries(c.merged.conflicts.map((x) => [x.pointer, 'theirs' as const])));
  }

  /** Keep mine on every conflicting field of the section ("Keep all mine"). */
  keepMine(section: Section): void {
    this.resolveSection(section, {});
  }

  /**
   * Decides a file's conflict: `theirs` takes their text (`theirsText`, read by the dialog; the
   * caller copies yours to the clipboard), `mine` overwrites it with yours.
   */
  resolveFile(path: string, choice: 'theirs' | 'mine', theirsText?: string): void {
    const c = this.state.fileConflicts[path];
    if (!c) return;
    const { [path]: _done, ...rest } = this.state.fileConflicts;
    this.set({ fileConflicts: rest });
    if (choice === 'theirs') {
      this.setFile(path, { server: theirsText ?? null, serverSha: c.theirsSha, text: theirsText ?? '', dirty: false });
      return;
    }
    this.setFile(path, { serverSha: c.theirsSha, server: null, dirty: true });
    void this.send(fileKey(path));
  }

  // Saving.

  private schedule(key: Key): void {
    clearTimeout(this.timers.get(key));
    this.timers.set(
      key,
      setTimeout(() => {
        this.timers.delete(key);
        void this.send(key);
      }, this.debounceMs),
    );
  }

  private scheduleDirty(): void {
    for (const s of sections) if (this.state.sections[s].dirty) this.schedule(s);
    for (const [p, f] of this.state.files) if (f.dirty) this.schedule(fileKey(p));
  }

  private blocked(key: Key): boolean {
    const s = this.state;
    if (s.discarded || this.disposed) return true;
    if (key.startsWith('file:')) return key.slice(5) in s.fileConflicts;
    return s.parseError !== null || key in s.conflicts || s.error !== null;
  }

  private dirty(key: Key): boolean {
    if (key.startsWith('file:')) return this.state.files.get(key.slice(5))?.dirty ?? false;
    return this.state.sections[key as Section].dirty;
  }

  /** Sends one section or file now, unless it is in flight (it is sent again after) or blocked. */
  private send(key: Key): Promise<void> {
    clearTimeout(this.timers.get(key));
    this.timers.delete(key);
    const running = this.inflight.get(key);
    if (running) return running.then(() => (this.dirty(key) ? this.send(key) : undefined));
    if (!this.dirty(key) || this.blocked(key)) return Promise.resolve();
    const p = (key.startsWith('file:') ? this.sendFile(key.slice(5)) : this.sendSection(key as Section)).finally(() => {
      this.inflight.delete(key);
      this.settle();
    });
    this.inflight.set(key, p);
    return p;
  }

  /** Opens the draft before the first save of a session that showed the running version. */
  private ensureDraft(): Promise<void> {
    if (this.state.hasDraft) return Promise.resolve();
    this.opening ??= (async () => {
      await this.api.openDraft(this.opts.org, this.opts.factory);
      const fresh = await this.api.getDraftModel(this.opts.org, this.opts.factory);
      const m = modelOf(fresh.model);
      // A draft this session opens is not the one it saved before (a discard restarts revisions at 1).
      this.set({ hasDraft: true, revision: fresh.revision, lastSaved: 0, edited: fresh.edited, ranges: fresh.ranges });
      for (const s of sections) {
        const cur = this.state.sections[s];
        if (cur.dirty && !equal(cur.server, m[s])) this.rebase(s, m[s], fresh.etags[s]);
        else this.setSection(s, { serverEtag: fresh.etags[s], ...(cur.dirty ? {} : { server: m[s], local: m[s] }) });
      }
    })().finally(() => {
      this.opening = undefined;
    });
    return this.opening;
  }

  /** Opens the draft when needed; a failure is that save's failure (retried like it). */
  private async opened(key: Key): Promise<boolean> {
    try {
      await this.ensureDraft();
      return true;
    } catch (err) {
      this.failedSave(key, err);
      return false;
    }
  }

  private async sendSection(section: Section): Promise<void> {
    if (!(await this.opened(section))) return;
    const cur = this.state.sections[section];
    if (!cur.dirty || this.blocked(section)) return;
    const sent = cur.local;
    const pending = this.pendingFiles.get(section);
    this.pendingFiles.delete(section);
    const save: SectionSave = {
      value: sent,
      ...(pending && Object.keys(pending.files).length > 0 ? { files: pending.files } : {}),
      ...(pending && pending.remove.size > 0 ? { remove_files: [...pending.remove] } : {}),
    };
    this.setSection(section, { inFlight: true });
    try {
      const result = await this.api.saveDraftSection(this.opts.org, this.opts.factory, section, cur.serverEtag, save);
      const now = this.state.sections[section];
      this.setSection(section, {
        server: sent,
        serverEtag: result.etag ?? now.serverEtag,
        inFlight: false,
        dirty: !equal(now.local, sent) || this.pendingFiles.has(section),
      });
      this.saved(result);
      if (this.state.sections[section].dirty) this.schedule(section);
    } catch (err) {
      this.restorePending(section, pending);
      this.setSection(section, { inFlight: false });
      this.failedSave(section, err);
    }
  }

  private restorePending(section: Section, pending: { files: FileMap; remove: Set<string> } | undefined): void {
    if (!pending) return;
    const later = this.pendingFiles.get(section);
    this.pendingFiles.set(section, {
      files: { ...pending.files, ...later?.files },
      remove: new Set([...pending.remove, ...(later?.remove ?? [])]),
    });
  }

  private async sendFile(path: string): Promise<void> {
    if (!(await this.opened(fileKey(path)))) return;
    const cur = this.state.files.get(path);
    if (!cur?.dirty || this.blocked(fileKey(path))) return;
    const sent = cur.text;
    this.setFile(path, { inFlight: true });
    try {
      const base = cur.serverSha ? { sha256: cur.serverSha } : ({ create: true } as const);
      const result = await this.api.putDraftFile(this.opts.org, this.opts.factory, path, sent, base);
      const now = this.state.files.get(path);
      this.setFile(path, {
        server: sent,
        serverSha: result.sha256 ?? null,
        inFlight: false,
        dirty: (now?.text ?? sent) !== sent,
      });
      this.saved(result);
      if (this.state.files.get(path)?.dirty) this.schedule(fileKey(path));
    } catch (err) {
      this.setFile(path, { inFlight: false });
      if (err instanceof ApiError && err.status === 412) {
        const theirsSha = typeof err.problem.sha256 === 'string' ? err.problem.sha256 : null;
        if (theirsSha !== null && theirsSha === (await sha256(sent))) {
          this.setFile(path, { server: sent, serverSha: theirsSha, dirty: this.state.files.get(path)?.text !== sent });
          return;
        }
        this.set({ fileConflicts: { ...this.state.fileConflicts, [path]: { path, theirsSha, ours: sent } } });
        return;
      }
      this.failedSave(fileKey(path), err);
    }
  }

  /** A save goald took: the draft's revision, counts and tiles. */
  private saved(result: SaveResult): void {
    this.failed.clear();
    this.attempt = 0;
    clearTimeout(this.retryTimer);
    this.retryTimer = undefined;
    this.savedSinceRefresh = true;
    this.set({
      revision: Math.max(this.state.revision, result.revision),
      lastSaved: Math.max(this.state.lastSaved, result.revision),
      savedAt: result.updated_at,
      summaries: result.sections,
      changeCount: result.change_count,
      problemCount: result.problem_count,
      retryAt: null,
      error: null,
    });
  }

  /** Merges what goald has now (theirs) under our edits: sent again when disjoint, else the dialog. */
  private rebase(section: Section, theirs: SectionValues[Section], theirsEtag: string): void {
    const cur = this.state.sections[section];
    const merged = merge3<unknown>(cur.server, theirs, cur.local) as Merged<SectionValues[Section]>;
    if (merged.conflicts.length === 0) {
      this.setSection(section, { server: theirs, serverEtag: theirsEtag });
      this.setLocal(section, merged.value);
      return;
    }
    this.setSection(section, { local: merged.value });
    this.set({ conflicts: { ...this.state.conflicts, [section]: { section, merged, theirs, theirsEtag } } });
  }

  private failedSave(key: Key, err: unknown): void {
    if (err instanceof ApiError) {
      const p = err.problem;
      if (err.status === 412 && !key.startsWith('file:') && p.section !== undefined) {
        this.rebase(key as Section, p.value as SectionValues[Section], p.etag ?? '');
        if (this.state.sections[key as Section].dirty && !(key in this.state.conflicts)) void this.send(key);
        return;
      }
      if (err.code === 'yaml-invalid') {
        this.set({ parseError: p.parse_error ?? { line: 0, column: 0, message: err.detail ?? err.title } });
        return;
      }
      if (err.code === 'no-draft') {
        this.set({ discarded: true, hasDraft: false });
        return;
      }
      if (err.status < 500 && err.status !== 429) {
        this.set({ error: err });
        return;
      }
    }
    this.failed.add(key);
    const delay = backoff[Math.min(this.attempt, backoff.length - 1)] as number;
    this.attempt += 1;
    clearTimeout(this.retryTimer);
    this.retryTimer = setTimeout(() => this.retryNow(), delay);
    this.retryKind = err instanceof ApiError ? 'retrying' : 'offline';
    this.set({ retryAt: Date.now() + delay });
  }

  /** "Retry now": sends every failed save at once (and a refused one again). */
  retryNow(): void {
    clearTimeout(this.retryTimer);
    this.retryTimer = undefined;
    const keys = [...this.failed];
    this.failed.clear();
    this.set({ retryAt: null, error: null });
    for (const k of keys) void this.send(k);
    this.scheduleDirty();
  }

  /** Once nothing is in flight or waiting, the draft's queries are refetched (once per burst). */
  private settle(): void {
    if (this.inflight.size > 0 || this.timers.size > 0 || !this.savedSinceRefresh) return;
    this.savedSinceRefresh = false;
    this.opts.refresh?.();
  }

  /**
   * Sends everything waiting now (section change, blur, before navigating) and resolves when
   * nothing is in flight; saves that cannot go (conflict, held, retrying) stay unsaved.
   */
  async flush(): Promise<void> {
    for (let round = 0; round < 10; round++) {
      const keys: Key[] = [
        ...sections.filter((s) => this.state.sections[s].dirty),
        ...[...this.state.files].filter(([, f]) => f.dirty).map(([p]) => fileKey(p)),
      ];
      const sendable = keys.filter((k) => !this.blocked(k) && !this.failed.has(k));
      if (sendable.length === 0 && this.inflight.size === 0) return;
      await Promise.allSettled([...sendable.map((k) => this.send(k)), ...this.inflight.values()]);
    }
  }

  /** "Edited · Undo": puts the base version's value back at pointers of one section, through goald. */
  async revert(section: Section, pointers: string[]): Promise<void> {
    await this.flush();
    const cur = this.state.sections[section];
    const result = await this.api.revertDraftSection(
      this.opts.org,
      this.opts.factory,
      section,
      cur.serverEtag,
      pointers,
    );
    this.setSection(section, { serverEtag: result.etag ?? cur.serverEtag });
    this.saved(result);
    this.settle();
  }

  /**
   * "Discard draft…": deletes the open draft at this session's revision and drops every unsaved
   * edit; the editor then shows the running version (adoptVersion) and the next edit opens a new
   * draft. A 412 (the draft moved since) throws and keeps everything.
   */
  async discard(): Promise<void> {
    await this.api.discardDraft(this.opts.org, this.opts.factory, this.state.revision);
    this.closeDraft();
  }

  /**
   * The draft is gone by this tab's own hand (discarded, or published as the next version): the
   * editor shows the running version from now on, nothing waits, and the next edit opens a new
   * draft. Unlike a discard elsewhere, it is not a refusal to report.
   */
  closeDraft(): void {
    for (const t of this.timers.values()) clearTimeout(t);
    this.timers.clear();
    clearTimeout(this.retryTimer);
    this.retryTimer = undefined;
    this.pendingFiles.clear();
    this.failed.clear();
    this.undoStack = [];
    this.redoStack = [];
    const sectionsNext = {} as Record<Section, SectionState>;
    for (const s of sections) {
      const cur = this.state.sections[s];
      sectionsNext[s] = { ...cur, local: cur.server, dirty: false, inFlight: false, serverEtag: '' };
    }
    this.set({
      hasDraft: false,
      revision: 0,
      lastSaved: 0,
      sections: sectionsNext as SectionStates,
      files: new Map(),
      edited: {},
      summaries: [],
      changeCount: 0,
      problemCount: 0,
      retryAt: null,
      error: null,
      conflicts: {},
      fileConflicts: {},
      parseError: null,
      updatedElsewhereAt: null,
      discarded: false,
    });
  }

  /**
   * The guard's condition: something is unsaved, in flight, or waiting on a retry or a decision.
   * While factory.yaml does not parse, only Visual changes held back count: the text is saved.
   */
  hasUnsaved(): boolean {
    const s = this.state;
    if (s.discarded) return true;
    if (s.status === 'held') {
      return (
        sections.some((k) => s.sections[k].dirty || s.sections[k].inFlight) ||
        [...s.files.values()].some((f) => f.dirty || f.inFlight)
      );
    }
    return s.status !== 'saved';
  }

  /** A file deleted or renamed away from the draft: the session forgets its text. */
  forgetFile(path: string): void {
    if (!this.state.files.has(path)) return;
    clearTimeout(this.timers.get(fileKey(path)));
    this.timers.delete(fileKey(path));
    const files = new Map(this.state.files);
    files.delete(path);
    const { [path]: _gone, ...fileConflicts } = this.state.fileConflicts;
    this.set({ files, fileConflicts });
  }

  /** The editor mounted (again: React StrictMode unmounts and remounts once in development). */
  activate(): void {
    this.disposed = false;
    if (this.state.retryAt !== null) this.retryNow();
    else this.scheduleDirty();
  }

  /** The editor unmounted: timers stop and goald's answers are ignored until it mounts again. */
  dispose(): void {
    for (const t of this.timers.values()) clearTimeout(t);
    this.timers.clear();
    clearTimeout(this.retryTimer);
    this.disposed = true;
  }
}
