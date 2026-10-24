import { createHash } from 'node:crypto';
import { ApiError } from '@goal/api-client';
import { afterEach, beforeEach, describe, expect, it, onTestFinished, vi } from 'vitest';
import type { Draft, DraftModel, FactoryModel, SaveResult, Section, SectionSave } from '../../../api/types';
import type { LineSection, Model } from '../../../shared/definition/model';
import { addAgent, renameAgent, setBasics, setStationField } from './ops';
import { DraftSession, type SessionApi } from './session';

const model = (): Model => ({
  basics: { id: 'f1', name: 'goal', title: 'Goal' },
  line: {
    stations: [
      { id: 'inbox', kind: 'queue' },
      { id: 'build', kind: 'agent', agent: 'engineer', wip: 2 },
      { id: 'review', kind: 'agent', agent: 'engineer', wip: 1 },
      { id: 'done', kind: 'done' },
    ],
  },
  agents: { agents: [{ name: 'engineer', prompt: { path: './agents/engineer.md' } }] },
  connections: [],
  review: {},
});

const draftModel = (m: Model, revision: number, etags: Partial<Record<Section, string>> = {}): DraftModel => ({
  revision,
  model: m as unknown as FactoryModel,
  etags: { basics: 'b1', line: 'l1', agents: 'a1', connections: 'c1', review: 'r1', ...etags },
  edited: {},
  ranges: {},
});

interface Call {
  section: Section;
  etag: string;
  save: SectionSave;
  resolve: (r: SaveResult) => void;
  reject: (e: unknown) => void;
}

/** goald's draft routes as the session calls them: every call waits until the test answers it. */
function fakeApi() {
  const saves: Call[] = [];
  const files: {
    path: string;
    text: string;
    base: unknown;
    resolve: (r: SaveResult) => void;
    reject: (e: unknown) => void;
  }[] = [];
  let revision = 42;
  const api: SessionApi = {
    getDraftModel: vi.fn(async () => draftModel(model(), revision)),
    openDraft: vi.fn(async () => ({ revision: 1 }) as Draft),
    saveDraftSection: vi.fn(
      (_org: string, _f: string, section: Section, etag: string, save: SectionSave) =>
        new Promise<SaveResult>((resolve, reject) => saves.push({ section, etag, save, resolve, reject })),
    ),
    revertDraftSection: vi.fn(async () => result(++revision, 'reverted')),
    discardDraft: vi.fn(async () => undefined),
    putDraftFile: vi.fn(
      (_org: string, _f: string, path: string, text: string, base: unknown) =>
        new Promise<SaveResult>((resolve, reject) => files.push({ path, text, base, resolve, reject })),
    ),
    getDraftFile: vi.fn(async (_org: string, _f: string, path: string) => {
      throw new ApiError({
        title: 'Not Found',
        status: 404,
        code: 'file-not-found',
        detail: `${path} isn't in the draft`,
      });
    }),
    getVersionFile: vi.fn(async (_org: string, _f: string, _v: string, path: string) => {
      throw new ApiError({ title: 'Not Found', status: 404, code: 'file-not-found', detail: `${path} isn't there` });
    }),
  };
  const result = (rev: number, etag: string, sha256?: string): SaveResult => ({
    revision: rev,
    etag,
    ...(sha256 ? { sha256 } : {}),
    updated_at: '2026-10-24T14:32:00Z',
    change_count: 1,
    problem_count: 0,
    sections: [],
  });
  /** Answers the oldest pending section save with success at the next revision. */
  const ok = async (etag = `e${revision + 1}`) => {
    const call = saves.shift();
    if (!call) throw new Error('no save pending');
    revision += 1;
    call.resolve(result(revision, etag));
    await flushPromises();
    return call;
  };
  return { api, saves, files, ok, result, setRevision: (r: number) => (revision = r) };
}

const flushPromises = () => vi.advanceTimersByTimeAsync(0);

const conflict = (section: Section, value: unknown, etag: string) =>
  new ApiError({ title: 'Precondition Failed', status: 412, code: 'draft-conflict', section, value, etag });

const line = (s: DraftSession) => s.getSnapshot().sections.line.local as LineSection;
const shown = (s: DraftSession): Model => {
  const { sections: v } = s.getSnapshot();
  return {
    basics: v.basics.local,
    line: v.line.local,
    agents: v.agents.local,
    connections: v.connections.local,
    review: v.review.local,
  };
};
const wipOf = (l: LineSection, id: string) => l.stations?.find((s) => s.id === id)?.wip;

let fake: ReturnType<typeof fakeApi>;
let session: DraftSession;
let refresh: ReturnType<typeof vi.fn<() => void>>;

beforeEach(() => {
  vi.useFakeTimers();
  fake = fakeApi();
  refresh = vi.fn<() => void>();
  session = new DraftSession({ org: 'acme', factory: 'f1', api: fake.api, refresh });
  session.adopt(draftModel(model(), 42));
});

afterEach(() => {
  session.dispose();
  vi.useRealTimers();
});

describe('autosave', () => {
  it('saves a section 800 ms after its last edit, with its etag, once', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    await vi.advanceTimersByTimeAsync(500);
    session.edit(setStationField(line(session), 'build', 'wip', 4));
    await vi.advanceTimersByTimeAsync(799);
    expect(fake.saves).toHaveLength(0);

    await vi.advanceTimersByTimeAsync(1);

    expect(fake.saves).toHaveLength(1);
    expect(fake.saves[0]).toMatchObject({ section: 'line', etag: 'l1' });
    expect(wipOf(fake.saves[0]?.save.value as LineSection, 'build')).toBe(4);
  });

  it('debounces each section on its own and flushes at once when asked', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    session.edit(setBasics(session.getSnapshot().sections.basics.local, 'title', 'Goal 2'));

    const flushed = session.flush();
    await flushPromises();

    expect(fake.saves.map((c) => c.section).sort()).toEqual(['basics', 'line']);
    await fake.ok();
    await fake.ok();
    await flushed;
    expect(session.getSnapshot()).toMatchObject({ status: 'saved', revision: 44 });
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it('sends the files an edit carries in the same section save', async () => {
    session.edit(addAgent(session.getSnapshot().sections.agents.local, 'reviewer'));
    void session.flush();
    await flushPromises();
    expect(fake.saves[0]?.save.files).toEqual({ 'agents/reviewer.md': { text: '# reviewer\n' } });
  });

  it('keeps one request in flight per section, then sends the latest value on the new etag', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    void session.flush();
    await flushPromises();
    session.edit(setStationField(line(session), 'build', 'wip', 5));
    void session.flush();
    await vi.advanceTimersByTimeAsync(1000);
    expect(fake.saves).toHaveLength(1);

    await fake.ok('l2');
    await vi.advanceTimersByTimeAsync(800);

    expect(fake.saves).toHaveLength(1);
    expect(fake.saves[0]?.etag).toBe('l2');
    expect(wipOf(fake.saves[0]?.save.value as LineSection, 'build')).toBe(5);
  });

  it('keeps local edits over a refetched model while they are unsaved, and adopts the rest', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    const remote = model();
    (remote.line.stations?.[2] as { wip: number }).wip = 9;
    remote.basics.title = 'Renamed elsewhere';

    session.adopt(draftModel(remote, 43, { line: 'l9', basics: 'b9' }));

    const snap = session.getSnapshot();
    expect(wipOf(line(session), 'build')).toBe(3);
    expect(wipOf(line(session), 'review')).toBe(1); // ours stays as it was, merged on save
    expect(snap.sections.line.serverEtag).toBe('l1');
    expect(snap.sections.basics).toMatchObject({
      local: { title: 'Renamed elsewhere' },
      serverEtag: 'b9',
      dirty: false,
    });
  });
});

describe('stale reads', () => {
  it('keeps a section this tab saved over a model read before the save committed', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 7));
    void session.flush();
    await flushPromises();
    await fake.ok('l43');

    // A refetch that started before the save (another tab's event, a focus refetch) answers late.
    session.adopt(draftModel(model(), 42));

    const snap = session.getSnapshot();
    expect(wipOf(line(session), 'build')).toBe(7);
    expect(snap.sections.line).toMatchObject({ serverEtag: 'l43', dirty: false });
    expect(snap.revision).toBe(43);
    expect(fake.api.saveDraftSection).toHaveBeenCalledTimes(1);
  });

  it('keeps a section adopted at a later revision over an older read of it', () => {
    const later = model();
    later.basics.title = 'Renamed at 45';
    session.adopt(draftModel(later, 45, { basics: 'b45' }));

    const earlier = model();
    earlier.basics.title = 'Renamed at 44';
    session.adopt(draftModel(earlier, 44, { basics: 'b44' }));

    expect(session.getSnapshot().sections.basics).toMatchObject({
      local: { title: 'Renamed at 45' },
      serverEtag: 'b45',
    });
  });
});

describe('conflicts', () => {
  it('merges a disjoint change made elsewhere and sends again', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    void session.flush();
    await flushPromises();
    const theirs = model().line;
    (theirs.stations?.[2] as { wip: number }).wip = 4;
    fake.saves.shift()?.reject(conflict('line', theirs, 'l7'));
    await flushPromises();

    expect(fake.saves).toHaveLength(1);
    expect(fake.saves[0]?.etag).toBe('l7');
    const sent = fake.saves[0]?.save.value as LineSection;
    expect([wipOf(sent, 'build'), wipOf(sent, 'review')]).toEqual([3, 4]);
  });

  it('asks when the same field changed elsewhere, and keeps theirs when told', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    void session.flush();
    await flushPromises();
    const theirs = model().line;
    (theirs.stations?.[1] as { wip: number }).wip = 4;
    fake.saves.shift()?.reject(conflict('line', theirs, 'l7'));
    await flushPromises();

    const snap = session.getSnapshot();
    expect(snap.status).toBe('conflict');
    expect(snap.conflicts.line?.merged.conflicts).toMatchObject([
      { path: ['stations', 'build', 'wip'], theirs: 4, ours: 3 },
    ]);
    expect(session.hasUnsaved()).toBe(true);
    expect(fake.saves).toHaveLength(0);

    session.resolveSection('line', { '/stations/1/wip': 'theirs' });
    await vi.advanceTimersByTimeAsync(1000);

    expect(wipOf(line(session), 'build')).toBe(4);
    expect(session.getSnapshot()).toMatchObject({ status: 'saved', conflicts: {} });
    expect(fake.saves).toHaveLength(0); // theirs is what goald has: nothing to send
  });

  it('overwrites with mine on the etag goald gave', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    void session.flush();
    await flushPromises();
    const theirs = model().line;
    (theirs.stations?.[1] as { wip: number }).wip = 4;
    fake.saves.shift()?.reject(conflict('line', theirs, 'l7'));
    await flushPromises();

    session.resolveSection('line', {});
    void session.flush();
    await flushPromises();

    expect(fake.saves[0]?.etag).toBe('l7');
    expect(wipOf(fake.saves[0]?.save.value as LineSection, 'build')).toBe(3);
  });

  it('asks before overwriting a file changed elsewhere', async () => {
    session.adoptFile('agents/engineer.md', { text: 'old\n', sha256: 'aaa' });
    session.editFile('agents/engineer.md', 'mine\n');
    void session.flush();
    await flushPromises();
    expect(fake.files[0]).toMatchObject({ path: 'agents/engineer.md', text: 'mine\n', base: { sha256: 'aaa' } });

    fake.files
      .shift()
      ?.reject(
        new ApiError({ title: 'x', status: 412, code: 'draft-conflict', path: 'agents/engineer.md', sha256: 'bbb' }),
      );
    await vi.waitFor(() =>
      expect(session.getSnapshot().fileConflicts['agents/engineer.md']).toEqual({
        path: 'agents/engineer.md',
        theirsSha: 'bbb',
        ours: 'mine\n',
      }),
    );

    session.resolveFile('agents/engineer.md', 'mine');
    await flushPromises();
    expect(fake.files[0]).toMatchObject({ text: 'mine\n', base: { sha256: 'bbb' } });
  });
});

describe('failures', () => {
  it('retries a save goald could not take, backing off from 1 s', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    void session.flush();
    await flushPromises();
    fake.saves.shift()?.reject(new TypeError('fetch failed'));
    await flushPromises();
    expect(session.getSnapshot()).toMatchObject({ status: 'offline', retryAt: Date.now() + 1000 });

    await vi.advanceTimersByTimeAsync(1000);
    fake.saves.shift()?.reject(new ApiError({ title: 'Bad Gateway', status: 502 }));
    await flushPromises();
    expect(session.getSnapshot()).toMatchObject({ status: 'retrying', retryAt: Date.now() + 2000 });

    await vi.advanceTimersByTimeAsync(2000);
    await fake.ok();
    expect(session.getSnapshot()).toMatchObject({ status: 'saved', retryAt: null });
    expect(session.hasUnsaved()).toBe(false);
  });

  it('holds sections while factory.yaml does not parse, and sends them once it does', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    void session.flush();
    await flushPromises();
    const parse_error = { line: 12, column: 3, message: 'did not find expected key' };
    fake.saves.shift()?.reject(new ApiError({ title: 'Conflict', status: 409, code: 'yaml-invalid', parse_error }));
    await flushPromises();
    expect(session.getSnapshot()).toMatchObject({ status: 'held', parseError: parse_error });
    await vi.advanceTimersByTimeAsync(5000);
    expect(fake.saves).toHaveLength(0);

    session.adopt(draftModel(model(), 50, { line: 'l50' }));
    await vi.advanceTimersByTimeAsync(800);

    expect(session.getSnapshot().parseError).toBeNull();
    expect(fake.saves[0]?.etag).toBe('l1'); // still our base: goald says whether it moved
  });

  it('notices a draft discarded elsewhere', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    void session.flush();
    await flushPromises();
    fake.saves.shift()?.reject(new ApiError({ title: 'No draft', status: 404, code: 'no-draft' }));
    await flushPromises();
    expect(session.getSnapshot().discarded).toBe(true);
    expect(session.hasUnsaved()).toBe(true);
  });
});

describe('the unsaved-changes guard', () => {
  it('lets go while factory.yaml does not parse once its text is saved, but not with Visual changes held', async () => {
    const parse_error = { line: 12, column: 3, message: 'did not find expected key' };
    session.adoptParseError(parse_error);
    expect(session.getSnapshot().status).toBe('held');
    expect(session.hasUnsaved()).toBe(false);

    session.editFile('factory.yaml', 'spec: [');
    expect(session.hasUnsaved()).toBe(true);
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    void session.flush();
    await flushPromises();
    fake.files.shift()?.resolve(fake.result(43, '', 'sha43'));
    await flushPromises();
    expect(session.hasUnsaved()).toBe(true); // the WIP waits for factory.yaml to parse
  });

  it('holds while anything is dirty or in flight, and lets go once saved', async () => {
    expect(session.hasUnsaved()).toBe(false);
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    expect(session.hasUnsaved()).toBe(true);
    void session.flush();
    await flushPromises();
    expect(session.getSnapshot().sections.line.inFlight).toBe(true);
    expect(session.hasUnsaved()).toBe(true);
    await fake.ok();
    expect(session.hasUnsaved()).toBe(false);
  });
});

describe('remote saves', () => {
  it("says the draft was updated elsewhere, but not for this tab's own save", async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    void session.flush();
    await flushPromises();
    await fake.ok();
    session.adopt(draftModel(model(), 43));
    expect(session.getSnapshot().updatedElsewhereAt).toBeNull();

    session.adopt(draftModel(model(), 44));
    expect(session.getSnapshot().updatedElsewhereAt).not.toBeNull();
  });

  it('says so for a save elsewhere in a new draft, whose revisions start again at 1', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    void session.flush();
    await flushPromises();
    await fake.ok();
    session.adoptMissing();
    session.restart();
    fake.setRevision(1);
    session.edit(setBasics(session.getSnapshot().sections.basics.local, 'title', 'Goal 2'));
    void session.flush();
    await flushPromises();
    await fake.ok();
    expect(session.getSnapshot()).toMatchObject({ hasDraft: true, revision: 2, updatedElsewhereAt: null });

    session.adopt(draftModel(model(), 3));

    expect(session.getSnapshot().updatedElsewhereAt).not.toBeNull();
  });
});

describe('without a draft', () => {
  it('opens one on the first save, then saves on its etags', async () => {
    const s = new DraftSession({ org: 'acme', factory: 'f1', api: fake.api });
    s.adoptVersion({ model: model() as unknown as FactoryModel, ranges: {} });
    expect(s.getSnapshot().hasDraft).toBe(false);

    s.edit(setStationField(s.getSnapshot().sections.line.local, 'build', 'wip', 3));
    void s.flush();
    await flushPromises();

    expect(fake.api.openDraft).toHaveBeenCalledOnce();
    expect(fake.saves[0]).toMatchObject({ section: 'line', etag: 'l1' });
    s.dispose();
  });
});

describe('without a draft, offline', () => {
  it('retries opening the draft like a save', async () => {
    vi.mocked(fake.api.openDraft).mockRejectedValueOnce(new TypeError('fetch failed'));
    const s = new DraftSession({ org: 'acme', factory: 'f1', api: fake.api });
    s.adoptVersion({ model: model() as unknown as FactoryModel, ranges: {} });
    s.edit(setStationField(s.getSnapshot().sections.line.local, 'build', 'wip', 3));
    void s.flush();
    await flushPromises();
    expect(s.getSnapshot().status).toBe('offline');

    await vi.advanceTimersByTimeAsync(1000);

    expect(fake.api.openDraft).toHaveBeenCalledTimes(2);
    expect(fake.saves).toHaveLength(1);
    s.dispose();
  });
});

describe('undo', () => {
  it('puts the value before the last edit back and saves it', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));
    session.edit(setStationField(line(session), 'build', 'wip', 4));
    expect(session.undo()).toBe(true);
    expect(wipOf(line(session), 'build')).toBe(3);
    expect(session.redo()).toBe(true);
    expect(wipOf(line(session), 'build')).toBe(4);
    session.undo();
    session.undo();
    expect(session.getSnapshot()).toMatchObject({ canUndo: false, canRedo: true });
    expect(session.getSnapshot().sections.line.dirty).toBe(false);
  });

  it('moves a renamed agent’s prompt back and forth with the rename, at goald and in the editor', async () => {
    // The rename saves the line too (engineer's stations): every save is answered in turn.
    const saved = async () => {
      void session.flush();
      await flushPromises();
      const agents = fake.saves.find((c) => c.section === 'agents')?.save;
      while (fake.saves.length > 0) await fake.ok();
      return agents;
    };
    session.adoptFile('agents/engineer.md', { text: '# engineer\n', sha256: 'aaa' });
    session.edit(renameAgent(shown(session), 'engineer', 'builder', '# engineer\n'));
    await saved();

    session.undo();
    expect(session.getSnapshot().files.get('agents/engineer.md')).toMatchObject({ text: '# engineer\n', dirty: false });
    expect(session.getSnapshot().files.has('agents/builder.md')).toBe(false);
    expect(await saved()).toMatchObject({
      files: { 'agents/engineer.md': { text: '# engineer\n' } },
      remove_files: ['agents/builder.md'],
    });

    session.redo();
    expect(session.getSnapshot().files.get('agents/builder.md')).toMatchObject({ text: '# engineer\n' });
    expect(session.getSnapshot().files.has('agents/engineer.md')).toBe(false);
    expect(await saved()).toMatchObject({
      files: { 'agents/builder.md': { text: '# engineer\n' } },
      remove_files: ['agents/engineer.md'],
    });
  });
});

describe('undoing a removal', () => {
  // engineer works at stations, so its removal is written as the agents edit Remove… makes.
  const removeEngineer = () =>
    session.edit({ section: 'agents', value: { agents: [] }, removeFiles: ['agents/engineer.md'] });

  it('writes back a removed file the session had not loaded, read from goald before the removal', async () => {
    vi.mocked(fake.api.getDraftFile).mockResolvedValueOnce({ text: '# engineer\n\nBuild it.\n', sha256: 'aaa' });
    expect(await session.fileText('agents/engineer.md')).toBe('# engineer\n\nBuild it.\n');
    removeEngineer();
    void session.flush();
    await flushPromises();
    expect(fake.saves[0]?.save).toMatchObject({ remove_files: ['agents/engineer.md'] });
    await fake.ok();

    session.undo();
    expect(session.getSnapshot().files.get('agents/engineer.md')).toMatchObject({ text: '# engineer\n\nBuild it.\n' });
    void session.flush();
    await flushPromises();
    expect(fake.saves[0]?.save).toMatchObject({
      files: { 'agents/engineer.md': { text: '# engineer\n\nBuild it.\n' } },
    });
  });

  it('reads a file only when it has no text of it, and the running version’s without a draft', async () => {
    session.editFile('agents/engineer.md', '# engineer, typed\n');
    expect(await session.fileText('agents/engineer.md')).toBe('# engineer, typed\n');
    expect(fake.api.getDraftFile).not.toHaveBeenCalled();

    const s = new DraftSession({ org: 'acme', factory: 'f1', api: fake.api });
    s.adoptVersion({ model: model() as unknown as FactoryModel, ranges: {} });
    vi.mocked(fake.api.getVersionFile).mockResolvedValueOnce({ text: '# engineer\n', sha256: 'aaa' });
    expect(await s.fileText('agents/engineer.md')).toBe('# engineer\n');
    expect(fake.api.getVersionFile).toHaveBeenCalledWith('acme', 'f1', 'current', 'agents/engineer.md');
    expect(s.getSnapshot().files.get('agents/engineer.md')).toMatchObject({ text: '# engineer\n', dirty: false });
    s.dispose();
  });

  it('refuses when goald cannot give the text, so the caller removes nothing', async () => {
    vi.mocked(fake.api.getDraftFile).mockRejectedValueOnce(new TypeError('Failed to fetch'));
    await expect(session.fileText('agents/engineer.md')).rejects.toThrow('Failed to fetch');
    expect(session.getSnapshot().files.has('agents/engineer.md')).toBe(false);
  });
});

describe('typing', () => {
  it('is one undo step per field until another edit comes between', () => {
    const basics = () => session.getSnapshot().sections.basics.local;
    for (const title of ['G', 'Go', 'Goa']) session.edit(setBasics(basics(), 'title', title), { coalesce: 'title' });
    session.edit(setBasics(basics(), 'purpose', 'Builds'), { coalesce: 'purpose' });
    session.edit(setBasics(basics(), 'title', 'Goal 2'), { coalesce: 'title' });

    session.undo();
    expect(basics().title).toBe('Goa');
    session.undo();
    expect([basics().title, basics().purpose]).toEqual(['Goa', undefined]);
    session.undo();
    expect(basics().title).toBe('Goal');
  });
});

describe('discard', () => {
  it('drops unsaved edits and the draft; the next edit opens a new one', async () => {
    session.edit(setStationField(line(session), 'build', 'wip', 3));

    await session.discard();

    expect(fake.api.discardDraft).toHaveBeenCalledWith('acme', 'f1', 42);
    expect(session.getSnapshot()).toMatchObject({ hasDraft: false, status: 'saved', changeCount: 0, canUndo: false });
    expect(wipOf(line(session), 'build')).toBe(2);
    await vi.advanceTimersByTimeAsync(1000);
    expect(fake.saves).toHaveLength(0);

    session.adoptVersion({ model: model() as unknown as FactoryModel, ranges: {} });
    session.edit(setStationField(line(session), 'build', 'wip', 4));
    void session.flush();
    await flushPromises();
    expect(fake.api.openDraft).toHaveBeenCalledOnce();
  });

  it('keeps the draft when it moved since (412)', async () => {
    vi.mocked(fake.api.discardDraft).mockRejectedValueOnce(
      new ApiError({ title: 'Precondition Failed', status: 412, code: 'draft-conflict' }),
    );

    await expect(session.discard()).rejects.toBeInstanceOf(ApiError);

    expect(session.getSnapshot().hasDraft).toBe(true);
  });
});

describe('publish', () => {
  it('closes the draft without calling it discarded elsewhere; the next edit opens a new one', async () => {
    session.closeDraft();
    session.adoptMissing();

    expect(session.getSnapshot()).toMatchObject({ hasDraft: false, discarded: false, status: 'saved' });
    expect(session.hasUnsaved()).toBe(false);
    session.adoptVersion({ model: model() as unknown as FactoryModel, ranges: {} });
    session.edit(setStationField(line(session), 'build', 'wip', 4));
    void session.flush();
    await flushPromises();
    expect(fake.api.openDraft).toHaveBeenCalledOnce();
  });
});

describe('files', () => {
  it('keeps a file this tab saved over a read of it that started before the save', async () => {
    session.adoptFile('factory.yaml', { text: 'kind: Factory\n', sha256: 'aaa' });
    session.editFile('factory.yaml', 'kind: Factory: x\n');
    void session.flush();
    await flushPromises();
    // The YAML view mounting again with the cached read, or a refetch that started before the save.
    const before = Date.now();
    await vi.advanceTimersByTimeAsync(5);
    fake.files.shift()?.resolve(fake.result(43, 'e43', 'bbb'));
    await flushPromises();

    session.adoptFile('factory.yaml', { text: 'kind: Factory\n', sha256: 'aaa', readAt: before });

    expect(session.getSnapshot().files.get('factory.yaml')).toMatchObject({
      text: 'kind: Factory: x\n',
      serverSha: 'bbb',
      dirty: false,
    });
  });

  it('takes a read that started after the save, even of text the file had before', async () => {
    session.adoptFile('factory.yaml', { text: 'agent: spec-writer\n', sha256: 'aaa' });
    session.editFile('factory.yaml', 'agent: ghost\n');
    void session.flush();
    await flushPromises();
    fake.files.shift()?.resolve(fake.result(43, 'e43', 'bbb'));
    await flushPromises();
    await vi.advanceTimersByTimeAsync(5);

    // A fix saved through the Line section put the text back as it was.
    session.adoptFile('factory.yaml', { text: 'agent: spec-writer\n', sha256: 'aaa', readAt: Date.now() });

    expect(session.getSnapshot().files.get('factory.yaml')).toMatchObject({
      text: 'agent: spec-writer\n',
      serverSha: 'aaa',
    });
  });

  it('shows a file an edit writes before its save lands, and forgets one it removes', () => {
    session.adoptFile('agents/engineer.md', { text: '# engineer\n', sha256: 'aaa' });

    session.edit(renameAgent(shown(session), 'engineer', 'builder', '# engineer\n'));

    expect(fake.saves).toHaveLength(0);
    const files = session.getSnapshot().files;
    expect(files.get('agents/builder.md')).toMatchObject({ text: '# engineer\n', dirty: false });
    expect(files.has('agents/engineer.md')).toBe(false);
  });

  it('saves typing into a file an edit wrote over the text that edit saved, even before its sha256 is computed', async () => {
    // WebCrypto answers off the microtask queue: the test decides when, so typing can come first.
    let release = () => {};
    const held = new Promise<void>((r) => {
      release = r;
    });
    const digest = vi.spyOn(crypto.subtle, 'digest').mockImplementation(async (_alg, data) => {
      await held;
      return new Uint8Array(
        createHash('sha256')
          .update(data as Uint8Array)
          .digest(),
      ).buffer;
    });
    onTestFinished(() => digest.mockRestore());

    session.edit(addAgent(session.getSnapshot().sections.agents.local, 'reviewer'));
    void session.flush();
    await flushPromises();
    await fake.ok();

    session.editFile('agents/reviewer.md', '# reviewer\n\nReview the change.\n');
    void session.flush();
    await flushPromises();
    release();
    await flushPromises();

    const sha = createHash('sha256').update('# reviewer\n').digest('hex');
    expect(fake.files[0]).toMatchObject({ path: 'agents/reviewer.md', base: { sha256: sha } });
  });

  it('forgets a deleted file and its pending save', async () => {
    session.editFile('agents/old.md', '# old\n');
    session.forgetFile('agents/old.md');

    await vi.advanceTimersByTimeAsync(1000);
    expect(fake.files).toHaveLength(0);
    expect(session.getSnapshot().files.has('agents/old.md')).toBe(false);
    expect(session.hasUnsaved()).toBe(false);
  });
});

describe('mounting', () => {
  it('takes goald answers again after an unmount and a remount (React StrictMode)', () => {
    const s = new DraftSession({ org: 'acme', factory: 'f1', api: fake.api });
    s.activate();
    s.dispose();
    s.activate();

    s.adopt(draftModel(model(), 7));

    expect(s.getSnapshot()).toMatchObject({ ready: true, revision: 7 });
    s.dispose();
  });
});

describe('read-only', () => {
  it('shows what it adopts and changes nothing, locally or at goald', async () => {
    const s = new DraftSession({ org: 'acme', factory: 'f1', api: fake.api, readOnly: true });
    s.adoptVersion({ model: model() as unknown as FactoryModel, ranges: {} });
    const before = s.getSnapshot();

    s.edit(setStationField(line(s), 'build', 'wip', 9));
    s.editFile('agents/engineer.md', '# changed');
    expect(s.undo()).toBe(false);
    expect(s.redo()).toBe(false);
    await s.revert('line', ['/line/stations/1/wip']);
    await s.discard();
    s.restart();
    await s.flush();
    await vi.advanceTimersByTimeAsync(5000);

    expect(s.getSnapshot()).toBe(before);
    expect(wipOf(line(s), 'build')).toBe(2);
    expect(s.hasUnsaved()).toBe(false);
    expect(fake.api.openDraft).not.toHaveBeenCalled();
    expect(fake.saves).toHaveLength(0);
    expect(fake.files).toHaveLength(0);
    expect(fake.api.revertDraftSection).not.toHaveBeenCalled();
    expect(fake.api.discardDraft).not.toHaveBeenCalled();
    s.dispose();
  });
});
