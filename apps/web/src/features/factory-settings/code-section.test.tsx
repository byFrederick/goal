// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  defKeys,
  draftFileQuery,
  draftFilesQuery,
  factoryRepositoryTestsQuery,
  membersQuery,
  orgQuery,
} from '../../api/queries';
import { listFactoryRepositoryTests, testFactoryRepository } from '../../api/repositories';
import type { Factory, FactoryRepositoryTest, OrgSummary } from '../../api/types';
import { factoryModelOf, type Model, modelOf } from '../../shared/definition/model';
import { renderFactoryPage } from '../../test/route';
import { FactorySettingsPage } from './settings-page';

const api = vi.hoisted(() => ({ model: {} as unknown }));

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
  getVersionModel: vi.fn(async () => ({ model: api.model, revision: 1 }) as never),
}));
vi.mock(import('../../api/repositories'), async (original) => ({
  ...(await original()),
  listFactoryRepositoryTests: vi.fn(async () => []),
  testFactoryRepository: vi.fn(),
}));

afterEach(cleanup);
beforeEach(() => vi.clearAllMocks());

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factoryId = 'factory-1';

const kept: FactoryRepositoryTest = {
  url: 'github.com/acme/app',
  access: 'write',
  secret: 'github-token',
  at: '2026-11-01T10:00:00Z',
  tested: true,
  ok: true,
  summary: 'Can read and push',
  note: "The host reports no pull-request permission, so Goal can't check it",
  read: { ok: true, reason: 'ok' },
  push: { ok: true, reason: 'ok' },
} as FactoryRepositoryTest;

function setDraft(basics: Partial<Model['basics']>, agents: Model['agents']['agents'] = [{ name: 'builder' }]) {
  const model: Model = {
    basics: { name: 'sre', purpose: 'Triage.', size: 'medium', tools: [], work_types: ['incident'], ...basics },
    line: { stations: [], flow: { release: 'push', targets: {} } },
    agents: { agents },
    connections: [],
    review: { budget: { weekly_usd: 50, on_exhausted: 'drain' } },
  } as Model;
  api.model = factoryModelOf(model);
}

async function renderCode({
  canEdit = true,
  results = [kept],
  seedResults = true,
}: {
  canEdit?: boolean;
  results?: FactoryRepositoryTest[];
  seedResults?: boolean;
} = {}) {
  const factory = {
    id: factoryId,
    org_id: org.id,
    name: 'sre',
    title: 'SRE',
    description: '',
    work_types: ['incident'],
    stations: [],
    current: { id: 'v1', number: 1 },
    draft: { revision: 1 },
    paused: false,
    created_at: '2026-10-01T00:00:00Z',
    role: 'editor',
    can: canEdit ? ['definition:edit'] : [],
  } as unknown as Factory;
  const seed: [readonly unknown[], unknown][] = [
    [orgQuery(org.id).queryKey, { ...org, created_at: '2026-10-01T00:00:00Z', can: [] }],
    [defKeys.factory(org.id, factoryId), factory],
    [draftFilesQuery(org.id, factoryId).queryKey, [{ path: 'factory.yaml', sha256: 'f1' }]],
    [
      draftFileQuery(org.id, factoryId, 'factory.yaml').queryKey,
      { path: 'factory.yaml', sha256: 'f1', text: 'spec: {}\n' },
    ],
    [membersQuery(org.id).queryKey, []],
  ];
  if (seedResults) seed.push([factoryRepositoryTestsQuery(org.id, factoryId).queryKey, results]);
  renderFactoryPage({ page: () => <FactorySettingsPage />, path: 'settings', org, factory, seed, topBar: true });
  return screen.findByRole('region', { name: 'Code' });
}

const entry = { name: 'app', url: 'https://github.com/acme/app', token: '{{github-token}}', access: 'write' as const };

describe('Code section', () => {
  it('shows each repository with its access, token by name and kept result', async () => {
    setDraft({ repositories: [entry] });
    const region = await renderCode();
    expect(await within(region).findByText('Can read and push')).toBeTruthy();
    expect(within(region).getByText('github.com/acme/app')).toBeTruthy();
    expect(within(region).getByText('Read and write')).toBeTruthy();
    expect(within(region).getByText('github-token')).toBeTruthy();
    expect(within(region).getByText(/can't check it/)).toBeTruthy();
  });

  it('shows the runtime.repository shorthand as one write entry', async () => {
    setDraft({ repository: { url: 'https://github.com/acme/app', token: '{{github-token}}' } });
    const region = await renderCode();
    expect(await within(region).findByText('Read and write')).toBeTruthy();
    expect(within(region).queryByRole('button', { name: 'Remove' })).toBeNull();
  });

  it('Test replaces the kept result, and a failed test shows its summary with the rework tone', async () => {
    setDraft({ repositories: [entry] });
    vi.mocked(testFactoryRepository).mockResolvedValue({
      ...kept,
      ok: false,
      note: 'x',
      summary: 'Cannot push to acme/app',
    } as FactoryRepositoryTest);
    const region = await renderCode();
    await within(region).findByText('Can read and push');
    fireEvent.click(within(region).getByRole('button', { name: 'Test' }));
    const chip = await within(region).findByText('Cannot push to acme/app');
    expect(vi.mocked(testFactoryRepository).mock.calls[0]?.slice(2)).toEqual([
      { url: 'https://github.com/acme/app', secret: 'github-token', access: 'write' },
    ]);
    expect(chip.closest('[class*="bg-rework-tint"]')).toBeTruthy();
    expect(within(region).queryByText('Can read and push')).toBeNull();
  });

  it('shows the CI line under the result, as a warning when a station waits for CI, and none without ci', async () => {
    setDraft({ repositories: [entry] });
    api.model = factoryModelOf({
      ...modelOf(api.model as never),
      line: { stations: [{ id: 'build', kind: 'agent', waitFor: 'ci' }] },
    } as Model);
    const failed = { ok: false, reason: 'GitHub answered 403: needs Checks and Commit statuses' };
    const region = await renderCode({ results: [{ ...kept, ci: failed }] });
    const line = await within(region).findByText(/Can’t read CI\. GitHub answered 403/);
    expect(line.className).toContain('text-rework-ink');
    expect(within(region).getByRole('button', { name: 'About reading CI' })).toBeTruthy();
    cleanup();
    setDraft({ repositories: [entry] });
    const plain = await renderCode({ results: [kept] });
    await within(plain).findByText('Can read and push');
    expect(within(plain).queryByText(/CI/)).toBeNull();
  });

  it('hides Test and Add a repository without definition:edit', async () => {
    setDraft({ repositories: [entry] });
    const region = await renderCode({ canEdit: false });
    await within(region).findByText('Can read and push');
    expect(within(region).queryByRole('button', { name: 'Test' })).toBeNull();
    expect(within(region).queryByRole('button', { name: 'Add a repository' })).toBeNull();
  });

  it('lists the agents that write: All agents, or the ones not set to read', async () => {
    setDraft({ repositories: [entry] }, [{ name: 'builder' }, { name: 'reviewer', code: 'read' }]);
    const region = await renderCode();
    expect(await within(region).findByText('builder')).toBeTruthy();
    expect(within(region).getByRole('link', { name: 'Agents' }).getAttribute('href')).toContain('/agents');
    cleanup();
    setDraft({ repositories: [entry] });
    const again = await renderCode();
    expect(await within(again).findByText('All agents')).toBeTruthy();
  });

  it('shows the branch names, the default branch as not available and the owner or admin tip', async () => {
    setDraft({ repositories: [entry] });
    const region = await renderCode();
    expect(await within(region).findByDisplayValue('goal/<number>-<slug>')).toBeTruthy();
    expect(within(region).getByRole('textbox', { name: 'Default branch' }).getAttribute('placeholder')).toBe(
      'Not available yet',
    );
    expect(within(region).getByRole('button', { name: 'About Secrets and webhooks' })).toBeTruthy();
    // On a phone each ⓘ is 44 px and a Remove button takes the 44 px minimum of a small button.
    for (const tip of within(region).getAllByRole('button', { name: /^About / })) {
      expect(tip.className).toContain('max-md:size-11');
    }
  });

  it('Add a repository opens the editor with a new entry', async () => {
    setDraft({ repositories: [entry] });
    const region = await renderCode();
    await within(region).findByText('Can read and push');
    fireEvent.click(within(region).getByRole('button', { name: 'Add a repository' }));
    await waitFor(() => expect(within(region).getByRole('button', { name: 'Done' })).toBeTruthy());
    expect(within(region).getAllByLabelText('URL').length).toBe(2);
  });

  it('Add a repository on the shorthand moves it into the list instead of setting both forms', async () => {
    setDraft({ repository: { url: 'https://github.com/acme/app', token: '{{github-token}}' } });
    const region = await renderCode();
    await within(region).findByText('Read and write');
    fireEvent.click(within(region).getByRole('button', { name: 'Add a repository' }));
    await waitFor(() => expect(within(region).getByRole('button', { name: 'Done' })).toBeTruthy());
    const urls = within(region).getAllByLabelText('URL') as HTMLInputElement[];
    expect(urls.map((u) => u.value)).toEqual(['https://github.com/acme/app', '']);
  });

  it('with no repositories says No code, offers Add and makes no test request', async () => {
    setDraft({});
    const region = await renderCode({ seedResults: false });
    expect(await within(region).findByText('No code. Agents work through their tools.')).toBeTruthy();
    expect(within(region).getByRole('button', { name: 'Add a repository' })).toBeTruthy();
    expect(listFactoryRepositoryTests).not.toHaveBeenCalled();
  });

  it('shows a skeleton per row while the kept results load', async () => {
    setDraft({ repositories: [entry] });
    vi.mocked(listFactoryRepositoryTests).mockReturnValue(new Promise(() => undefined));
    const region = await renderCode({ seedResults: false });
    await within(region).findByText('github.com/acme/app');
    await waitFor(() => expect(region.querySelector('li [aria-busy="true"]')).toBeTruthy());
  });
});
