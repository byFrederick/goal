// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { listMembers } from '../../api/client';
import { defKeys, membersQuery, orgQuery } from '../../api/queries';
import type { Factory, Member, Org, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { FactorySettingsPage } from './settings-page';

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  listMembers: vi.fn(async () => []),
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
async function renderSettings({ can = [], members }: { can?: string[]; members?: Member[] } = {}) {
  const seed: [readonly unknown[], unknown][] = [
    [orgQuery(org.id).queryKey, orgWith(can)],
    [defKeys.factory(org.id, factory.id), factory],
  ];
  if (members) seed.push([membersQuery(org.id).queryKey, members]);
  const rendered = renderFactoryPage({ page: () => <FactorySettingsPage />, path: 'settings', org, factory, seed });
  await screen.findByRole('heading', { name: 'Settings' });
  return rendered;
}

describe('Factory settings page', () => {
  it('shows General, People with access and the Danger zone with its rows', async () => {
    await renderSettings({ can: ['factory:archive', 'factory:delete'], members: [member('Freddy Cruz', 'owner')] });

    expect(screen.getByRole('heading', { name: 'General' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'People with access' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Danger zone' })).toBeTruthy();
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('sre');
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
    expect(screen.getByLabelText('Name')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Pause' })).toBeTruthy();
  });

  it('shows only the rows the caller may use: no Archive or Delete without factory:archive', async () => {
    await renderSettings({ can: [], members: [] });

    expect(screen.getByRole('button', { name: 'Pause' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Archive' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull();
  });
});
