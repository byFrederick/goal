// @vitest-environment jsdom
// The trigger page as a person sees and uses it: its blocks, its four states, the disabled controls,
// the save (its calls in order, then a clean form), the problems placed under their fields, and a
// new trigger from the New trigger picker. The API boundary is mocked; the routes and the shell are not.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import * as definitions from '../../api/definitions';
import * as intake from '../../api/intake';
import { defKeys } from '../../api/queries';
import type { DefinitionProblem, Factory, IntakeTriggerDetail, Problem } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { TriggerPage } from './trigger-page';
import { keyLines } from './trigger-yaml';

const route = vi.hoisted(() => ({
  params: { trigger: 'flaky-test-on-main' } as { trigger: string },
  search: {} as { on?: string },
}));

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>();
  return {
    ...actual,
    useParams: () => route.params,
    useSearch: () => route.search,
    Link: ({ children }: { children: ReactNode }) => <a href="/definition">{children}</a>,
  };
});

// The top bar renders the page's actions inline here, where the app puts them in the shell's top bar.
vi.mock('../../app/top-bar', () => ({
  TopBarSlot: ({ actions }: { actions?: ReactNode }) => <div>{actions}</div>,
}));

vi.mock('../../shared/org', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/org')>()),
  useOrg: () => org,
}));

vi.mock('../../shared/factory', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/factory')>()),
  useFactory: () => factory,
}));

vi.mock('../../api/intake', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/intake')>()),
  getIntakeTrigger: vi.fn(),
}));

vi.mock('../../api/definitions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/definitions')>()),
  getFactory: vi.fn(),
  getDraft: vi.fn(),
  getDraftFile: vi.fn(),
  getVersionFile: vi.fn(),
  openDraft: vi.fn(),
  putDraftFile: vi.fn(),
  validateDraft: vi.fn(),
  publishDraft: vi.fn(),
}));

const org = { id: 'org-1', slug: 'acme', name: 'Acme' };

const factory = {
  id: 'factory-1',
  name: 'software',
  title: 'Software',
  current: { id: 'version-2', number: 2 },
  draft: null,
} as unknown as Factory;

const file = `# Software: turns intake into reviewed pull requests.
apiVersion: goal/v1
kind: Factory
metadata:
  id: 0c3e8f42-7a19-4d6b-9e25-5b1f0a7c3d88
  name: software
spec:
  board:
    workTypes: [feature, fix]
    stations:
      - { id: intake, kind: queue }
      - { id: build, kind: agent }
    triggers:
      - name: flaky-test-on-main
        on: github.check_run.failed
        filter: { branch: main, repo: byFrederick/goal }
        only: { failuresInARow: 2, dedupe: check.name }
        create: { type: fix, station: build, expedite: true, title: "Fix flaky {{check.name}} on main" }
        limits: { perDay: 3, pauseOnBudget: true }
`;

const detail: IntakeTriggerDetail = {
  trigger: {
    name: 'flaky-test-on-main',
    enabled: true,
    source: 'github',
    on: 'github.check_run.failed',
    when: 'github.check_run.failed',
    create: { type: 'fix', station: 'build', lane: null, expedite: true },
    days: [0, 0, 0, 0, 0, 1, 2],
    problem: null,
  },
  week: { created: 3, merged: 2, spend_usd: 7.8 },
  events: [
    {
      id: 'firing-1',
      title: 'TestLeaseExpiry',
      event: 'github.check_run.failed',
      at: '2026-10-27T14:05:00Z',
      outcome: 'created',
      item_number: 150,
      reason: null,
    },
    {
      id: 'firing-2',
      title: 'TestBoardStream',
      event: 'github.check_run.failed',
      at: '2026-10-26T18:20:00Z',
      outcome: 'merged',
      item_number: 133,
      reason: null,
    },
    {
      id: 'firing-3',
      title: 'TestLeaseExpiry',
      event: 'github.check_run.failed',
      at: '2026-10-23T09:00:00Z',
      outcome: 'skipped',
      item_number: null,
      reason: 'once',
    },
  ],
};

const ok = (extra: object = {}) => ({ ok: true, revision: 2, counts: {}, checks: [], problems: [], ...extra });

/** The file as the definition has it: a save replaces it, and the reads that follow see the new text. */
let stored: string;

const mocked = {
  getIntakeTrigger: vi.mocked(intake.getIntakeTrigger),
  getFactory: vi.mocked(definitions.getFactory),
  getDraft: vi.mocked(definitions.getDraft),
  getDraftFile: vi.mocked(definitions.getDraftFile),
  getVersionFile: vi.mocked(definitions.getVersionFile),
  openDraft: vi.mocked(definitions.openDraft),
  putDraftFile: vi.mocked(definitions.putDraftFile),
  validateDraft: vi.mocked(definitions.validateDraft),
  publishDraft: vi.mocked(definitions.publishDraft),
};

beforeEach(() => {
  stored = file;
  route.params = { trigger: 'flaky-test-on-main' };
  route.search = {};
  mocked.getFactory.mockResolvedValue(factory);
  mocked.getDraft.mockResolvedValue({ id: 'draft-1', revision: 1, change_count: 0 } as never);
  mocked.getVersionFile.mockImplementation(async () => ({ text: stored, sha256: `sha-${stored.length}` }));
  mocked.getDraftFile.mockImplementation(async () => ({ text: stored, sha256: `sha-${stored.length}` }));
  mocked.getIntakeTrigger.mockResolvedValue(detail);
  mocked.openDraft.mockResolvedValue({ id: 'draft-1', revision: 1, change_count: 0 } as never);
  mocked.putDraftFile.mockImplementation(async (_org, _factory, _path, text) => {
    stored = text;
    return {
      revision: 2,
      sha256: `sha-${text.length}`,
      updated_at: '2026-10-27T10:00:00Z',
      change_count: 1,
      problem_count: 0,
      sections: [],
    } as never;
  });
  mocked.validateDraft.mockResolvedValue(ok() as never);
  mocked.publishDraft.mockResolvedValue({ number: 3 } as never);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const view = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <TriggerPage />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { ...view, client };
}

const saveButton = () => screen.getByRole('button', { name: 'Save changes' });

describe('the ready page', () => {
  it('shows the trigger as the picture does: its title, the week, its blocks, its events and its YAML', async () => {
    renderPage();
    expect(await screen.findByRole('heading', { level: 1, name: 'Flaky test on main' })).toBeTruthy();
    expect(screen.getByRole('switch', { name: 'Flaky test on main on' }).getAttribute('aria-checked')).toBe('true');
    expect(screen.getByText('Created 3 items this week, 2 merged, $7.80 in all')).toBeTruthy();

    const when = screen.getByRole('region', { name: 'When' });
    expect(within(when).getByText('GitHub')).toBeTruthy();
    expect((within(when).getByRole('combobox', { name: 'Event' }) as HTMLSelectElement).value).toBe(
      'github.check_run.failed',
    );
    expect((within(when).getByRole('textbox', { name: 'Branch' }) as HTMLInputElement).value).toBe('main');
    expect((within(when).getByRole('textbox', { name: 'Repository' }) as HTMLInputElement).value).toBe(
      'byFrederick/goal',
    );
    expect(within(when).getByRole('button', { name: /More filters: paths, authors, skip bots/ })).toBeTruthy();

    const only = screen.getByRole('region', { name: 'Only if' });
    expect(within(only).getByText('No open item already covers this check')).toBeTruthy();
    expect((within(only).getByRole('spinbutton', { name: 'Failures in a row' }) as HTMLInputElement).value).toBe('2');

    const create = screen.getByRole('region', { name: 'Create' });
    expect((within(create).getByRole('combobox', { name: 'Item type' }) as HTMLSelectElement).value).toBe('fix');
    expect((within(create).getByRole('combobox', { name: 'Starts at station' }) as HTMLSelectElement).value).toBe(
      'build',
    );
    expect((within(create).getByRole('combobox', { name: 'Lane' }) as HTMLSelectElement).value).toBe('expedite');
    expect((within(create).getByRole('textbox', { name: 'Title' }) as HTMLInputElement).value).toBe(
      'Fix flaky {{check.name}} on main',
    );
    expect(
      within(create).getByText(
        'Text from the event reaches the agent marked as untrusted data, never as instructions.',
      ),
    ).toBeTruthy();

    const limits = screen.getByRole('region', { name: 'Limits' });
    expect((within(limits).getByRole('spinbutton', { name: 'Items a day' }) as HTMLInputElement).value).toBe('3');
    expect(within(limits).getByText("Pause when Software's weekly budget is spent")).toBeTruthy();

    const recent = screen.getByRole('region', { name: 'Recent events' });
    expect(within(recent).getByText('Created #150')).toBeTruthy();
    expect(within(recent).getByText('#133 merged')).toBeTruthy();
    expect(within(recent).getByText('Skipped, once')).toBeTruthy();

    const definition = screen.getByRole('region', { name: 'In the definition' });
    expect(within(definition).getByText('Open file')).toBeTruthy();
    expect(within(definition).getByText("Saving publishes a new version of Software's definition.")).toBeTruthy();
    expect(definition.querySelector('pre')?.textContent).toContain('- name: flaky-test-on-main');
  });

  it('disables the controls that are not built yet, with the tooltip that says so', async () => {
    renderPage();
    await screen.findByRole('heading', { level: 1 });
    for (const name of [/Try with a past event/, /A model agrees it is worth an item/, /Add another event/]) {
      expect(screen.getByRole('button', { name }).getAttribute('aria-disabled')).toBe('true');
    }
    fireEvent.focus(screen.getByRole('button', { name: /Try with a past event/ }));
    expect(await screen.findByText('Not available yet')).toBeTruthy();
    expect(screen.queryByText(/copy/i)).toBeNull();
  });
});

describe('the four states', () => {
  it('shows loading while the definition is read', async () => {
    mocked.getVersionFile.mockReturnValue(new Promise(() => {}));
    const { container } = renderPage();
    expect(container.querySelector('[aria-busy="true"]')).toBeTruthy();
    expect(screen.getByText('Loading')).toBeTruthy();
  });

  it('shows the empty state when the trigger has no events yet', async () => {
    mocked.getIntakeTrigger.mockResolvedValue({ ...detail, events: [], week: { created: 0, merged: 0, spend_usd: 0 } });
    renderPage();
    expect(await screen.findByText('No events yet')).toBeTruthy();
    expect(screen.getByText('No items created this week')).toBeTruthy();
  });

  it('shows an error with a retry when the trigger cannot be read', async () => {
    mocked.getIntakeTrigger.mockRejectedValue(new ApiError({ title: 'Server error', status: 500 } as Problem));
    renderPage();
    expect(await screen.findByText(/Could not load this trigger/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('says so when the definition has no trigger of that name', async () => {
    route.params = { trigger: 'missing' };
    renderPage();
    expect(await screen.findByText('No trigger named missing')).toBeTruthy();
  });

  it('says so when the trigger API answers trigger-not-found', async () => {
    mocked.getIntakeTrigger.mockRejectedValue(
      new ApiError({ title: 'Not found', status: 404, code: 'trigger-not-found' } as Problem),
    );
    renderPage();
    expect(await screen.findByText('No trigger named flaky-test-on-main')).toBeTruthy();
  });
});

describe('saving', () => {
  it('writes the trigger, then validates and publishes, in that order, and leaves a clean form', async () => {
    renderPage();
    await screen.findByRole('heading', { level: 1 });
    expect(saveButton().hasAttribute('disabled')).toBe(true);

    fireEvent.change(screen.getByRole('spinbutton', { name: 'Items a day' }), { target: { value: '5' } });
    fireEvent.click(saveButton());

    expect(await screen.findByText("Saved. Software's definition is version 3 now.")).toBeTruthy();
    const starts = [
      mocked.openDraft,
      mocked.getDraftFile,
      mocked.putDraftFile,
      mocked.validateDraft,
      mocked.publishDraft,
    ].map((m) => m.mock.invocationCallOrder[0] ?? Number.POSITIVE_INFINITY);
    expect([...starts].sort((a, b) => a - b)).toEqual(starts);

    expect(mocked.openDraft).toHaveBeenCalledWith('org-1', 'factory-1');
    const [, , path, text, base] = mocked.putDraftFile.mock.calls[0] ?? [];
    expect(path).toBe('factory.yaml');
    expect(text).toContain('limits: { perDay: 5, pauseOnBudget: true }');
    expect(base).toEqual({ sha256: `sha-${file.length}` });
    expect(mocked.publishDraft).toHaveBeenCalledWith(
      'org-1',
      'factory-1',
      expect.objectContaining({ revision: 2, expected_current_version_id: 'version-2' }),
    );
    expect(saveButton().hasAttribute('disabled')).toBe(true);
  });

  it('keeps the typed edits and says so when the definition changed under the save (412)', async () => {
    renderPage();
    await screen.findByRole('heading', { level: 1 });
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Items a day' }), { target: { value: '5' } });
    mocked.putDraftFile.mockRejectedValueOnce(
      new ApiError({ title: 'Draft conflict', status: 412, code: 'draft-conflict' } as Problem),
    );
    fireEvent.click(saveButton());

    expect(await screen.findByText(/The definition changed while you were editing/)).toBeTruthy();
    expect(mocked.publishDraft).not.toHaveBeenCalled();
    expect((screen.getByRole('spinbutton', { name: 'Items a day' }) as HTMLInputElement).value).toBe('5');
    expect(saveButton().hasAttribute('disabled')).toBe(false);
  });

  it('blocks the save while the open draft has edits of its own, with a link to the definition editor', async () => {
    mocked.getFactory.mockResolvedValue({ ...factory, draft: { id: 'draft-1', revision: 4 } } as unknown as Factory);
    mocked.getDraft.mockResolvedValue({ id: 'draft-1', revision: 4, change_count: 2 } as never);
    renderPage();
    await screen.findByRole('heading', { level: 1 });
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Items a day' }), { target: { value: '5' } });
    expect(await screen.findByText(/has an open draft with edits/)).toBeTruthy();
    expect(saveButton().hasAttribute('disabled')).toBe(true);
    expect(screen.getByRole('link', { name: 'Open the definition editor' })).toBeTruthy();
    expect(mocked.openDraft).not.toHaveBeenCalled();
  });

  it('places a validation problem under the field whose lines hold it, and keeps Save off while it stands', async () => {
    const limitsLine = keyLines(file, 'flaky-test-on-main').limits?.from ?? 0;
    const problem: DefinitionProblem = {
      code: 'minimum',
      severity: 'error',
      check: 'schema',
      section: 'line',
      file: 'factory.yaml',
      line: limitsLine,
      column: 1,
      end_line: limitsLine,
      end_column: 1,
      message: 'perDay must be at least 1',
      fixes: [],
    };
    mocked.validateDraft.mockResolvedValue(ok({ ok: false, problems: [problem] }) as never);
    renderPage();
    await screen.findByRole('heading', { level: 1 });
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Items a day' }), { target: { value: '5' } });
    fireEvent.click(saveButton());

    const limits = await screen.findByRole('region', { name: 'Limits' });
    expect(within(limits).getByRole('alert').textContent).toBe('perDay must be at least 1');
    expect(mocked.publishDraft).not.toHaveBeenCalled();
    expect(saveButton().hasAttribute('disabled')).toBe(true);
  });

  it("lets the fix save after an invalid save, even once the draft shows the page's own write", async () => {
    const problem: DefinitionProblem = {
      code: 'minimum',
      severity: 'error',
      check: 'schema',
      section: 'line',
      file: 'factory.yaml',
      line: keyLines(file, 'flaky-test-on-main').limits?.from ?? 0,
      column: 1,
      end_line: 1,
      end_column: 1,
      message: 'perDay must be at least 1',
      fixes: [],
    };
    // The draft is open, and it holds this page's write once the first save has put the file.
    mocked.getFactory.mockResolvedValue({ ...factory, draft: { id: 'draft-1', revision: 1 } } as unknown as Factory);
    mocked.getDraft.mockImplementation(
      async () => ({ id: 'draft-1', revision: 1, change_count: mocked.putDraftFile.mock.calls.length }) as never,
    );
    mocked.validateDraft.mockResolvedValueOnce(ok({ ok: false, problems: [problem] }) as never);
    const { client } = renderPage();
    await screen.findByRole('heading', { level: 1 });
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Items a day' }), { target: { value: '4' } });
    fireEvent.click(saveButton());
    expect(await screen.findByText('perDay must be at least 1')).toBeTruthy();

    // The stream refreshes the draft, so the page reads that it now has a change: the page's own write.
    await act(() => client.invalidateQueries({ queryKey: defKeys.draft('org-1', 'factory-1') }));
    await waitFor(() => expect(mocked.getDraft.mock.results.length).toBeGreaterThan(1));
    expect(screen.queryByText(/has an open draft with edits/)).toBeNull();

    fireEvent.change(screen.getByRole('spinbutton', { name: 'Items a day' }), { target: { value: '5' } });
    fireEvent.click(saveButton());
    expect(await screen.findByText("Saved. Software's definition is version 3 now.")).toBeTruthy();
    expect(mocked.publishDraft).toHaveBeenCalledTimes(1);
  });

  it('shows a problem outside every field above the form', async () => {
    const problem: DefinitionProblem = {
      code: 'schema',
      severity: 'error',
      check: 'schema',
      section: 'line',
      file: 'factory.yaml',
      line: 2,
      column: 1,
      end_line: 2,
      end_column: 1,
      message: 'apiVersion is not supported',
      fixes: [],
    };
    mocked.validateDraft.mockResolvedValue(ok({ ok: false, problems: [problem] }) as never);
    renderPage();
    await screen.findByRole('heading', { level: 1 });
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Items a day' }), { target: { value: '5' } });
    fireEvent.click(saveButton());
    expect(await screen.findByText('apiVersion is not supported')).toBeTruthy();
  });
});

describe('a new trigger from the New trigger picker', () => {
  it('opens a blank trigger for the event and adds it to the definition on save', async () => {
    route.params = { trigger: 'lint-main' };
    route.search = { on: 'github.check_run.failed' };
    mocked.getIntakeTrigger.mockRejectedValue(new ApiError({ title: 'Not found', status: 404 } as Problem));
    renderPage();
    expect(await screen.findByRole('heading', { level: 1, name: 'Lint main' })).toBeTruthy();
    expect(screen.getByText('Not in the definition yet. Save to add it.')).toBeTruthy();
    expect(saveButton().hasAttribute('disabled')).toBe(true);

    fireEvent.change(screen.getByRole('combobox', { name: 'Item type' }), { target: { value: 'fix' } });
    fireEvent.click(saveButton());
    expect(await screen.findByText(/Saved\./)).toBeTruthy();
    const [, , , text] = mocked.putDraftFile.mock.calls[0] ?? [];
    expect(text).toContain('- name: lint-main\n');
    expect(text).toContain('on: github.check_run.failed');
  });
});
