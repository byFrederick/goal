// @vitest-environment jsdom
import { act, cleanup, fireEvent, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { listMembers } from '../../api/client';
import { defKeys, membersQuery, orgQuery } from '../../api/queries';
import type { Factory, Member, Org, OrgSummary } from '../../api/types';
import { factoryModelOf, type Model } from '../../shared/definition/model';
import { renderFactoryPage } from '../../test/route';
import { FactorySettingsPage } from './settings-page';

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  listMembers: vi.fn(async () => []),
}));

const draftState = vi.hoisted(() => ({ changes: 0 }));

vi.mock(import('../../api/definitions'), async (original) => ({
  ...(await original()),
  getDraft: vi.fn(
    async () =>
      ({
        id: 'draft-1',
        revision: 1,
        next_number: 4,
        change_count: draftState.changes,
        problem_count: 0,
        base: { id: 'v3', number: 3 },
        current: { id: 'v3', number: 3 },
        sections: [{ section: 'basics', changes: draftState.changes }],
      }) as never,
  ),
  getDraftModel: vi.fn(
    async () =>
      ({
        model: factoryModelOf({
          basics: { name: 'sre', purpose: 'Triage.', size: 'medium', tools: [], work_types: ['incident'] },
          line: { stations: [], flow: { release: 'push', targets: {} } },
          agents: { agents: [] },
          connections: [],
          review: { budget: { weekly_usd: 50, on_exhausted: 'drain' } },
        } as Model),
        etags: { basics: 'b1', line: 'l1', agents: 'a1', connections: 'c1', review: 'r1' },
        revision: 1,
        edited: {},
        ranges: {},
      }) as never,
  ),
}));

vi.mock(import('../../api/org-notifications'), async (original) => ({
  ...(await original()),
  getOrgNotificationSettings: vi.fn(async () => ({ slack_secret: 'slack-bot' })),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(listMembers).mockResolvedValue([]);
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'sre',
  title: 'SRE',
  description: 'Triage, investigate and document production issues.',
  work_types: ['incident'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['factory:manage'],
};

const member = (name: string, role: Member['role']): Member => ({
  member_id: `m-${name}`,
  user: { id: `u-${name}`, name, email: `${name.toLowerCase().replace(' ', '.')}@example.com` },
  role,
  factory_roles: [],
  joined_at: '2026-10-01T00:00:00Z',
  last_active_at: null,
  you: false,
  can_change_role: false,
  can_remove: false,
});

const orgWith = (can: string[]): Org => ({ ...org, created_at: '2026-10-01T00:00:00Z', can });

/** Renders the settings page with its factory, the organization's `can` and its members (or none seeded). */
async function renderSettings({
  can = [],
  members,
  withDraft = false,
}: {
  can?: string[];
  members?: Member[];
  withDraft?: boolean;
} = {}) {
  const open: Factory = withDraft
    ? {
        ...factory,
        current: {
          id: 'v3',
          number: 3,
          published_at: '2026-10-02T00:00:00Z',
          published_by: { kind: 'user' as const, id: 'u', name: 'Ada' },
        },
        draft: {
          id: 'd1',
          revision: 1,
          updated_at: '2026-11-08T10:00:00Z',
          updated_by: { kind: 'user' as const, id: 'u', name: 'Ada' },
        },
        can: [...factory.can, 'definition:edit'],
      }
    : factory;
  const seed: [readonly unknown[], unknown][] = [
    [orgQuery(org.id).queryKey, orgWith(can)],
    [defKeys.factory(org.id, factory.id), open],
  ];
  if (members) seed.push([membersQuery(org.id).queryKey, members]);
  const rendered = renderFactoryPage({
    page: () => <FactorySettingsPage />,
    path: 'settings',
    org,
    factory: open,
    seed,
    topBar: true,
  });
  await screen.findByRole('heading', { name: 'Settings' });
  return rendered;
}

describe('Factory settings page', () => {
  it('shows the purpose and a read-only badge with the sidebar’s letter', async () => {
    await renderSettings();

    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('SRE');
    expect((screen.getByLabelText('Purpose') as HTMLTextAreaElement).value).toBe(
      'Triage, investigate and document production issues.',
    );
    const row = screen.getByText('Badge').parentElement as HTMLElement;
    expect(within(row).getByText('S')).toBeTruthy();
    expect(within(row).queryByRole('textbox')).toBeNull();
  });

  it('has flat sections with no disclosure buttons and no Save changes for a caller who cannot edit', async () => {
    await renderSettings();

    expect(document.querySelector('[aria-expanded]')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Save changes' })).toBeNull();
  });

  it('marks the section in view in the On this page nav', async () => {
    let notify: IntersectionObserverCallback = () => {};
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        constructor(cb: IntersectionObserverCallback) {
          notify = cb;
        }
        observe() {}
        disconnect() {}
      },
    );
    try {
      await renderSettings();
      const nav = screen.getByRole('navigation', { name: 'On this page' });
      expect(within(nav).getByRole('link', { name: 'General' }).getAttribute('aria-current')).toBe('true');

      const target = document.getElementById('flow') as HTMLElement;
      act(() => notify([{ isIntersecting: true, target } as unknown as IntersectionObserverEntry], {} as never));

      expect(within(nav).getByRole('link', { name: 'Flow' }).getAttribute('aria-current')).toBe('true');
      expect(within(nav).getByRole('link', { name: 'General' }).getAttribute('aria-current')).toBeNull();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('shows General, People with access and the Danger zone with its rows', async () => {
    await renderSettings({ can: ['factory:archive', 'factory:delete'], members: [member('Freddy Cruz', 'owner')] });

    expect(screen.getByRole('heading', { name: 'General' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'People with access' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Danger zone' })).toBeTruthy();
    expect((screen.getByLabelText('Name (in URLs and the CLI)') as HTMLInputElement).value).toBe('sre');
    expect((screen.getByLabelText('Purpose') as HTMLTextAreaElement).readOnly).toBe(true);
    expect(screen.getByText('Agents read this as their first line of context.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Pause' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Archive' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Delete' })).toBeTruthy();
  });

  it('lists each member with the role the organization gives them', async () => {
    const viewer = {
      ...member('Jon Lee', 'member'),
      factory_roles: [{ factory_id: factory.id, role: 'viewer' as const }],
    };
    await renderSettings({ members: [member('Freddy Cruz', 'owner'), viewer] });

    const people = screen.getByRole('region', { name: 'People with access' });
    expect(within(people).getByText('Freddy Cruz')).toBeTruthy();
    expect(within(people).getByText('Organization owner')).toBeTruthy();
    expect(within(people).getByText('Owner')).toBeTruthy();
    expect(within(people).getByText('Jon Lee')).toBeTruthy();
    expect(within(people).getByText('Viewer')).toBeTruthy();
  });

  it('disables Add people for a caller who may not manage members', async () => {
    await renderSettings({ members: [member('Freddy Cruz', 'owner')] });
    const add = screen.getByRole('button', { name: 'Add people' });
    expect((add as HTMLButtonElement).disabled).toBe(true);
  });

  it('shows the section empty line when no one has access', async () => {
    await renderSettings({ members: [] });

    expect(screen.getByText('No people with access yet.')).toBeTruthy();
  });

  it('shows Loading in People with access while the members load', async () => {
    vi.mocked(listMembers).mockReturnValue(new Promise(() => {}));
    await renderSettings();

    const people = screen.getByRole('region', { name: 'People with access' });
    expect(within(people).getByText('Loading')).toBeTruthy();
    expect(people.getAttribute('aria-busy')).toBe('true');
  });

  it('shows an error in People with access and still renders the rest of the page', async () => {
    vi.mocked(listMembers).mockRejectedValue(new Error('network down'));
    await renderSettings();

    const people = await screen.findByRole('region', { name: 'People with access' });
    expect(await within(people).findByRole('alert')).toBeTruthy();
    expect(screen.getByLabelText('Name (in URLs and the CLI)')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Pause' })).toBeTruthy();
  });

  it('shows only the rows the caller may use: no Archive or Delete without factory:archive', async () => {
    await renderSettings({ can: [], members: [] });

    expect(screen.getByRole('button', { name: 'Pause' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Archive' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull();
  });

  it('shows the sections in order and lists the same ones in On this page', async () => {
    await renderSettings({ can: ['factory:archive', 'factory:delete'], members: [] });

    const ids = Array.from(document.querySelectorAll('section[id]')).map((section) => section.id);
    expect(ids).toEqual([
      'general',
      'code',
      'runtime',
      'budgets',
      'flow',
      'jonah',
      'notifications',
      'people',
      'danger',
    ]);
    const nav = screen.getByRole('navigation', { name: 'On this page' });
    expect(
      within(nav)
        .getAllByRole('link')
        .map((link) => link.textContent),
    ).toEqual([
      'General',
      'Code',
      'Runtime',
      'Budgets and targets',
      'Flow',
      'Jonah',
      'Notifications',
      'People with access',
      'Danger zone',
    ]);
  });

  it('shows Loading in each settings group while the draft loads', async () => {
    await renderSettings({ members: [] });

    for (const name of ['Code', 'Runtime', 'Budgets and targets', 'Flow']) {
      const group = screen.getByRole('region', { name });
      expect(within(group).getByText('Loading')).toBeTruthy();
      expect(group.querySelector('[aria-busy="true"]')).toBeTruthy();
    }
  });

  it('shows the open draft with its changes, Review changes and Discard draft', async () => {
    draftState.changes = 2;
    await renderSettings({ withDraft: true });
    const banner = await screen.findByRole('region', { name: 'Draft' });
    expect(within(banner).getByText('Draft of v4, 2 changes')).toBeTruthy();
    expect(within(banner).getByRole('link', { name: 'Review changes' }).getAttribute('href')).toContain(
      '/definition/review',
    );
    fireEvent.click(within(banner).getByRole('button', { name: 'Discard draft' }));
    expect(await screen.findByRole('dialog')).toBeTruthy();
  });

  it('shows no draft banner without changes or without a draft', async () => {
    draftState.changes = 0;
    await renderSettings({ withDraft: true });
    await screen.findByRole('heading', { name: 'Settings' });
    expect(screen.queryByRole('region', { name: 'Draft' })).toBeNull();
    cleanup();
    await renderSettings();
    expect(screen.queryByRole('region', { name: 'Draft' })).toBeNull();
  });
});
