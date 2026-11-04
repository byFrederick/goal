// @vitest-environment jsdom
import { act, cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { publishDraft } from '../../api/definitions';
import {
  defKeys,
  draftFileQuery,
  draftFilesQuery,
  membersQuery,
  orgQuery,
  repositoryTestQuery,
  secretsQuery,
} from '../../api/queries';
import type { Factory, OrgSummary, Secret, SectionSave } from '../../api/types';
import { factoryModelOf, type Model, type Section } from '../../shared/definition/model';
import { renderFactoryPage } from '../../test/route';
import { FactorySettingsPage } from './settings-page';

// Goald answers the draft and takes its saves; the settings groups write through the same session the editor
// uses, so each test reads what the session saved for a section.
/** The fields of the sections the settings write, as the session saved them. */
interface Saved {
  repository?: { url?: string; connection?: string; token?: string };
  size?: string;
  tools?: string[];
  work_types?: string[];
  budget: { [key: string]: unknown };
  targets?: Record<string, number>;
  flow: {
    release?: string;
    targets: Record<string, number>;
    expedite?: { types?: string[] };
    assemblies?: { overlap?: string; fresh_base?: string };
  };
}

const api = vi.hoisted(() => ({
  model: {} as Saved,
  saves: [] as { section: string; value: Saved }[],
  files: [] as { path: string; text: string }[],
}));

vi.mock(import('../../api/definitions'), async (original) => ({
  ...(await original()),
  getDraftModel: vi.fn(
    async () =>
      ({
        model: api.model,
        etags: { basics: 'b1', line: 'l1', agents: 'a1', connections: 'c1', review: 'r1' },
        revision: 1,
        edited: {},
        ranges: {},
      }) as never,
  ),
  saveDraftSection: vi.fn(
    async (_org: string, _factory: string, section: Section, _etag: string, save: SectionSave) => {
      api.saves.push({ section, value: save.value as Saved });
      return {
        etag: 'next',
        revision: 2,
        updated_at: '2026-10-28T10:00:00Z',
        sections: [],
        change_count: 1,
        problem_count: 0,
      };
    },
  ),
  putDraftFile: vi.fn(async (_org: string, _factory: string, path: string, text: string) => {
    api.files.push({ path, text });
    return {
      etag: 'next',
      revision: 2,
      updated_at: '2026-10-28T10:00:00Z',
      sha256: 'f2',
      sections: [],
      change_count: 1,
      problem_count: 0,
    };
  }),
  getVersionModel: vi.fn(async () => ({ model: api.model, revision: 1 }) as never),
  publishDraft: vi.fn(async () => ({ id: 'version-2', number: 2 }) as never),
}));

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  listMembers: vi.fn(async () => []),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  api.saves = [];
  api.files = [];
  setDraft();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factoryId = 'factory-1';

const can = ['factory:manage', 'definition:edit'];

function setDraft(over: Partial<Model> = {}) {
  const model: Model = {
    basics: {
      name: 'sre',
      purpose: 'Triage production issues.',
      size: 'medium',
      tools: ['node@22'],
      work_types: ['incident'],
      repository: { url: 'https://github.com/acme/sre', token: '{{github-token}}' },
    },
    line: {
      stations: [{ id: 'build', name: 'Build', kind: 'agent', agent: 'sre', wip: 2 }],
      flow: {
        release: 'push',
        expedite: { types: ['outage'] },
        assemblies: { overlap: 'serialize', fresh_base: 'merge' },
        targets: { throughput_per_day: 3 },
      },
    },
    agents: { agents: [{ name: 'sre' }] },
    connections: [],
    review: { budget: { weekly_usd: 50, on_exhausted: 'drain' } },
    ...over,
  };
  api.model = factoryModelOf(model) as unknown as Saved;
}

const secrets = [{ name: 'github-token' }, { name: 'slack-bot' }] as unknown as Secret[];
const passing = { tested: true, read: { ok: true, reason: 'ok' }, push: { ok: true, reason: 'ok' } };

function factoryDetail(): Factory {
  return {
    id: factoryId,
    org_id: 'org-1',
    name: 'sre',
    title: 'SRE',
    description: 'Triage production issues.',
    work_types: ['incident'],
    stations: [],
    current: { id: 'version-1', number: 1 } as Factory['current'],
    draft: { revision: 1 } as Factory['draft'],
    paused: false,
    created_at: '2026-10-01T00:00:00Z',
    role: 'editor',
    can,
  } as unknown as Factory;
}

async function renderSettings({ canEdit = true }: { canEdit?: boolean } = {}) {
  const factory = { ...factoryDetail(), can: canEdit ? can : [] };
  const seed: [readonly unknown[], unknown][] = [
    [orgQuery(org.id).queryKey, { ...org, created_at: '2026-10-01T00:00:00Z', can: ['factory:create'] }],
    [defKeys.factory(org.id, factoryId), factory],
    [secretsQuery(org.id).queryKey, secrets],
    [
      draftFilesQuery(org.id, factoryId).queryKey,
      [
        { path: 'factory.yaml', sha256: 'f1' },
        { path: 'environments/default.yaml', sha256: 'e1' },
        { path: 'environments/deploy.yaml', sha256: 'e2' },
      ],
    ],
    [
      draftFileQuery(org.id, factoryId, 'factory.yaml').queryKey,
      {
        path: 'factory.yaml',
        sha256: 'f1',
        text: 'spec:\n  runtime:\n    modelKey: "{{claude}}"\n  agents:\n    - name: sre\n    - name: deploy-bot\n      environment: deploy\nlearning:\n  # Jonah\n  jonah:\n    share: 8%\n',
      },
    ],
    [repositoryTestQuery(org.id, 'https://github.com/acme/sre', 'github-token').queryKey, passing],
    [membersQuery(org.id).queryKey, []],
  ];
  const rendered = renderFactoryPage({
    page: () => <FactorySettingsPage />,
    path: 'settings',
    org,
    factory,
    seed,
    topBar: true,
  });
  await screen.findByRole('region', { name: 'Repository' });
  await waitFor(() => expect(screen.getByText('github.com/acme/sre')).toBeTruthy(), { timeout: 3000 });
  if (canEdit) fireEvent.click(screen.getByRole('button', { name: 'Change' }));
  return rendered;
}

/** The last value the session saved for a section, once a save lands. */
async function savedSection(section: Section, check: (value: Saved) => boolean = () => true) {
  await waitFor(
    () => {
      const hit = api.saves.filter((s) => s.section === section).at(-1);
      expect(hit && check(hit.value)).toBe(true);
    },
    { timeout: 3000 },
  );
  return api.saves.filter((s) => s.section === section).at(-1)?.value as Saved;
}

describe('Repository', () => {
  it('shows the URL, Token works and the token secret, and Change opens the editor', async () => {
    await renderSettings({ canEdit: false });
    expect(screen.getByText('Token works')).toBeTruthy();
    expect(screen.getByText('github-token')).toBeTruthy();
    expect(screen.queryByText(/connect/i)).toBeNull();
    expect(screen.queryByLabelText('Repository URL')).toBeNull();
    cleanup();
    await renderSettings();
    expect(screen.getByLabelText('Repository URL')).toBeTruthy();
  });

  it('has no accordion buttons: every section is open', async () => {
    await renderSettings();
    expect(document.querySelector('[aria-expanded]:not([role="combobox"])')).toBeNull();
    expect(screen.getByLabelText('Each week')).toBeTruthy();
  });

  it('writes the repository URL to basics.repository.url', async () => {
    await renderSettings();
    const url = screen.getByLabelText('Repository URL');
    fireEvent.change(url, { target: { value: 'https://github.com/acme/tools' } });
    fireEvent.blur(url);

    const basics = await savedSection('basics', (v) => v.repository?.url === 'https://github.com/acme/tools');
    expect(basics.repository).toEqual({ url: 'https://github.com/acme/tools', token: '{{github-token}}' });
  });

  it('writes the token secret git signs in with to basics.repository.token, and none clears it', async () => {
    await renderSettings();
    expect(screen.queryByLabelText('Repository connection')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Use no token' }));

    const basics = await savedSection('basics', (v) => v.repository?.token === undefined);
    expect(basics.repository).toEqual({ url: 'https://github.com/acme/sre' });
  });

  it('shows Base branch as not available, with aria-disabled and no write', async () => {
    await renderSettings();
    const base = screen.getByRole('textbox', { name: 'Base branch' });
    expect(base.getAttribute('aria-disabled')).toBe('true');
    expect(base.getAttribute('placeholder')).toBe('Not available yet');
    fireEvent.change(base, { target: { value: 'develop' } });
    expect(api.saves.some((s) => s.section === 'basics' && 'base_branch' in s.value)).toBe(false);
  });
});

describe('Runtime', () => {
  it('shows the model key secret and one chip per environment with its agent count', async () => {
    await renderSettings();
    expect(await screen.findByText('claude')).toBeTruthy();
    expect(screen.getByText('default')).toBeTruthy();
    expect(screen.getByText('deploy')).toBeTruthy();
    expect(screen.getAllByText('1 agent')).toHaveLength(2);
    expect(screen.getByRole('link', { name: 'Edit' }).getAttribute('href')).toContain('/definition/environments');
    expect(screen.queryByText(/connection/i)).toBeNull();
  });

  it('points at environments instead of editing the box size and tools', async () => {
    await renderSettings();
    const runtime = screen.getByRole('region', { name: 'Runtime' });

    expect(within(runtime).getByText(/Box size, CLIs and what agents reach live in/)).toBeTruthy();
    const link = within(runtime).getByRole('link', { name: 'environments' });
    expect(link.getAttribute('href')).toBe('/o/acme/f/sre/definition/environments');
    expect(within(runtime).queryByRole('radio')).toBeNull();
    expect(within(runtime).queryByText('Tools')).toBeNull();
    expect(within(runtime).queryByPlaceholderText('name@version…')).toBeNull();
  });
});

describe('Budgets and targets', () => {
  it.each([
    ['Each day', 'daily_usd', '25'],
    ['Each week', 'weekly_usd', '120'],
    ['Each item', 'per_item_usd', '4.5'],
  ])('writes %s to review.budget.%s', async (label, key, typed) => {
    await renderSettings();
    const input = screen.getByLabelText(label);
    fireEvent.change(input, { target: { value: typed } });
    fireEvent.blur(input);

    const review = await savedSection('review', (v) => v.budget?.[key] !== undefined);
    expect(review.budget[key]).toBe(Number(typed));
    expect(review.budget.on_exhausted).toBe('drain');
  });

  it('writes turns per item to review.budget.max_turns_per_task', async () => {
    await renderSettings();
    const turns = screen.getByLabelText('Turns per item');
    fireEvent.change(turns, { target: { value: '40' } });
    fireEvent.blur(turns);

    const review = await savedSection('review', (v) => v.budget?.max_turns_per_task === 40);
    expect(review.budget.max_turns_per_task).toBe(40);
    expect(review.budget.weekly_usd).toBe(50);
  });

  it.each([
    ['Items a day', 'throughput_per_day', '6'],
    ['Hours lead time', 'lead_time_hours', '48'],
    ['Expedite lead time, minutes', 'expedite_lead_time_minutes', '90'],
  ])('writes %s to line.flow.targets.%s', async (label, key, typed) => {
    await renderSettings();
    fireEvent.change(screen.getByLabelText(label), { target: { value: typed } });

    const line = await savedSection('line', (v) => v.flow?.targets?.[key] === Number(typed));
    expect(line.flow.targets[key]).toBe(Number(typed));
    expect(line.flow.assemblies).toEqual({ overlap: 'serialize', fresh_base: 'merge' });
  });
});

describe('Flow', () => {
  it('writes When a budget runs out to review.budget.on_exhausted and keeps the budget', async () => {
    await renderSettings();
    fireEvent.click(screen.getByRole('radio', { name: 'Stop at once' }));

    const review = await savedSection('review', (v) => v.budget?.on_exhausted === 'stop');
    expect(review.budget).toEqual({ weekly_usd: 50, on_exhausted: 'stop' });
  });

  it('writes New work enters to line.flow.release and keeps the other flow keys', async () => {
    await renderSettings();
    fireEvent.click(screen.getByRole('radio', { name: 'Pull when the constraint has room' }));

    const line = await savedSection('line', (v) => v.flow?.release === 'drum-buffer-rope');
    expect(line.flow.release).toBe('drum-buffer-rope');
    expect(line.flow.targets).toEqual({ throughput_per_day: 3 });
    expect(line.flow.expedite).toEqual({ types: ['outage'] });
  });

  it('writes Items that touch the same files to assemblies.overlap and keeps fresh_base', async () => {
    await renderSettings();
    fireEvent.click(screen.getByRole('radio', { name: 'Work side by side' }));

    const line = await savedSection('line', (v) => v.flow?.assemblies?.overlap === 'allow');
    expect(line.flow.assemblies).toEqual({ overlap: 'allow', fresh_base: 'merge' });
  });

  it('stacks each choice as a labelled radio group below 768 px and still writes the draft', async () => {
    const original = window.matchMedia;
    window.matchMedia = ((query: string) =>
      ({
        matches: query.includes('max-width: 767px'),
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
      }) as unknown as MediaQueryList) as typeof window.matchMedia;
    try {
      await renderSettings();
      const group = screen.getByRole('group', { name: 'New work enters' });
      expect(group.getAttribute('aria-describedby')).toBeTruthy();
      expect(within(group).getAllByRole('radio')).toHaveLength(2);
      fireEvent.click(within(group).getByRole('radio', { name: 'Pull when the constraint has room' }));
      const line = await savedSection('line', (v) => v.flow?.release === 'drum-buffer-rope');
      expect(line.flow.release).toBe('drum-buffer-rope');
    } finally {
      window.matchMedia = original;
    }
  });

  it('appends a work type to line.flow.expedite.types', async () => {
    await renderSettings();
    fireEvent.change(screen.getByPlaceholderText('Work type…'), { target: { value: 'incident' } });
    fireEvent.keyDown(screen.getByLabelText('Add a type'), { key: 'Enter' });

    const line = await savedSection('line', (v) => (v.flow?.expedite?.types ?? []).includes('incident'));
    expect(line.flow.expedite?.types).toEqual(['outage', 'incident']);
  });
});

describe('Save changes', () => {
  it('publishes the draft at its revision and the current version id, then closes it', async () => {
    await renderSettings();
    fireEvent.click(screen.getByRole('radio', { name: 'Stop at once' }));
    await savedSection('review', (v) => v.budget?.on_exhausted === 'stop');

    const save = screen.getByRole('button', { name: 'Save changes' });
    expect(screen.getByText('Saving publishes v2')).toBeTruthy();
    await act(async () => fireEvent.click(save));

    await waitFor(() =>
      expect(publishDraft).toHaveBeenCalledWith(
        org.id,
        factoryId,
        expect.objectContaining({ expected_current_version_id: 'version-1' }),
      ),
    );
  });

  it('reads Saving… while the publish is pending, then shows Published', async () => {
    let finish: (v: never) => void = () => {};
    vi.mocked(publishDraft).mockImplementationOnce(() => new Promise((resolve) => (finish = resolve as never)));
    await renderSettings();
    fireEvent.click(screen.getByRole('radio', { name: 'Stop at once' }));
    await savedSection('review', (v) => v.budget?.on_exhausted === 'stop');
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Save changes' })));

    expect(await screen.findByRole('button', { name: 'Saving…' })).toBeTruthy();
    await act(async () => finish({ id: 'version-2', number: 2 } as never));
    expect(await screen.findByText('Published')).toBeTruthy();
  });

  it('shows goald’s refusal when the publish is refused', async () => {
    const { ApiError } = await import('../../api/client');
    vi.mocked(publishDraft).mockRejectedValueOnce(
      new ApiError({
        status: 409,
        title: 'Conflict',
        detail: 'The draft has open items at removed stations.',
      } as never),
    );
    await renderSettings();
    fireEvent.click(screen.getByRole('radio', { name: 'Stop at once' }));
    await savedSection('review', (v) => v.budget?.on_exhausted === 'stop');
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Save changes' })));

    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText(/open items at removed stations/)).toBeTruthy();
  });
});

describe('Read-only callers', () => {
  it('sees the groups read-only and no Save changes button', async () => {
    await renderSettings({ canEdit: false });

    expect(screen.queryByRole('button', { name: 'Save changes' })).toBeNull();
    expect(screen.getByText('github.com/acme/sre')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Change' })).toBeNull();
    expect(screen.getByLabelText('Each week').closest('fieldset')?.disabled).toBe(true);
  });
});

describe('Jonah', () => {
  it('shows learning.jonah and writes a change to factory.yaml, keeping its comments', async () => {
    await renderSettings();
    const share = (await screen.findByLabelText(/^Budget, %/)) as HTMLInputElement;
    await waitFor(() => expect(share.value).toBe('8'));
    expect(screen.getByRole('switch', { name: 'Jonah reviews this line' }).getAttribute('aria-checked')).toBe('true');
    fireEvent.click(screen.getByRole('switch', { name: 'Jonah reviews this line' }));
    await waitFor(() => expect(api.files.at(-1)?.text).toMatch(/enabled: false/), { timeout: 3000 });
    expect(api.files.at(-1)?.text).toMatch(/# Jonah/);
    const after = screen.getByLabelText(/^Proposes after, items/);
    fireEvent.change(after, { target: { value: '5' } });
    fireEvent.blur(after);
    await waitFor(() => expect(api.files.at(-1)?.text).toMatch(/proposeAfter: 5/), { timeout: 3000 });
  });

  it('flags a share over 100 and writes nothing', async () => {
    await renderSettings();
    const share = (await screen.findByLabelText(/^Budget, %/)) as HTMLInputElement;
    await waitFor(() => expect(share.value).toBe('8'));
    fireEvent.change(share, { target: { value: '150' } });
    expect(await screen.findByText('A number from 0 to 100.')).toBeTruthy();
    expect(api.files).toEqual([]);
  });

  it('is read-only without definition:edit', async () => {
    await renderSettings({ canEdit: false });
    expect(screen.getByLabelText(/^Proposes after, items/).closest('fieldset')?.disabled).toBe(true);
  });
});
