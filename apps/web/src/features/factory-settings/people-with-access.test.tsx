// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { toast } from 'sonner';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, listMembers, setFactoryRole } from '../../api/client';
import { defKeys, membersQuery, orgQuery } from '../../api/queries';
import type { Factory, Member, Org, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { FactorySettingsPage } from './settings-page';

vi.mock('../../api/client', async (original) => ({
  ...(await original()),
  listMembers: vi.fn(async () => []),
  setFactoryRole: vi.fn(async () => undefined),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(listMembers).mockResolvedValue([]);
  vi.mocked(setFactoryRole).mockResolvedValue(undefined);
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

const member = (name: string, role: Member['role'], factoryRole?: Member['factory_roles'][number]['role']): Member => ({
  member_id: `m-${name}`,
  user: { id: `u-${name}`, name, email: `${name.toLowerCase().replace(' ', '.')}@example.com` },
  role,
  factory_roles: factoryRole ? [{ factory_id: factory.id, role: factoryRole }] : [],
  joined_at: '2026-10-01T00:00:00Z',
  last_active_at: null,
  you: false,
  can_change_role: false,
  can_remove: false,
});

const roster = [
  member('Freddy Cruz', 'owner'),
  member('Maria Rojas', 'member', 'approver'),
  member('Jon Lee', 'member', 'viewer'),
  member('Sam Ortiz', 'member'),
];

/** Renders the settings page with the organization's `can` and its members. */
async function renderPeople({ can = [], members = roster }: { can?: string[]; members?: Member[] } = {}) {
  const orgWith: Org = { ...org, created_at: '2026-10-01T00:00:00Z', can };
  const seed: [readonly unknown[], unknown][] = [
    [orgQuery(org.id).queryKey, orgWith],
    [defKeys.factory(org.id, factory.id), factory],
    [membersQuery(org.id).queryKey, members],
  ];
  renderFactoryPage({ page: () => <FactorySettingsPage />, path: 'settings', org, factory, seed });
  await screen.findByRole('heading', { name: 'Settings' });
  return screen.getByRole('region', { name: 'People with access' });
}

const manage = ['member:manage'];

describe('People with access', () => {
  it('lists the owners, admins and members with a role here, each with its line and chip', async () => {
    const people = await renderPeople();

    expect(within(people).getByText('Freddy Cruz')).toBeTruthy();
    expect(within(people).getByText('Organization owner')).toBeTruthy();
    expect(within(people).getByText('Approves and decides')).toBeTruthy();
    expect(within(people).getByText('Approver')).toBeTruthy();
    expect(within(people).getByText('Sees the floor, creates items')).toBeTruthy();
    expect(within(people).getByText('Viewer')).toBeTruthy();
    // A member with no role on this factory is not one of its people.
    expect(within(people).queryByText('Sam Ortiz')).toBeNull();
  });

  it('shows the admin line and chip for an admin, who holds every role', async () => {
    const people = await renderPeople({
      members: [member('Ada Okafor', 'admin')],
    });

    expect(within(people).getByText('Organization admin')).toBeTruthy();
    expect(within(people).getByText('Admin')).toBeTruthy();
  });

  it('shows Loading while the members load', async () => {
    vi.mocked(listMembers).mockReturnValue(new Promise(() => {}));
    renderFactoryPage({
      page: () => <FactorySettingsPage />,
      path: 'settings',
      org,
      factory,
      seed: [
        [orgQuery(org.id).queryKey, { ...org, created_at: '2026-10-01T00:00:00Z', can: [] }],
        [defKeys.factory(org.id, factory.id), factory],
      ],
    });
    await screen.findByRole('heading', { name: 'Settings' });
    const people = screen.getByRole('region', { name: 'People with access' });
    expect(within(people).getByText('Loading')).toBeTruthy();
    expect(people.getAttribute('aria-busy')).toBe('true');
  });

  it('shows the empty line when no one has access to the factory', async () => {
    const people = await renderPeople({ members: [member('Sam Ortiz', 'member')] });

    expect(within(people).getByText('No people with access yet.')).toBeTruthy();
  });

  it('shows an error in the section and still renders the rest of the page', async () => {
    renderFactoryPage({
      page: () => <FactorySettingsPage />,
      path: 'settings',
      org,
      factory,
      seed: [
        [orgQuery(org.id).queryKey, { ...org, created_at: '2026-10-01T00:00:00Z', can: [] }],
        [defKeys.factory(org.id, factory.id), factory],
      ],
    });
    vi.mocked(listMembers).mockRejectedValue(new Error('network'));
    await screen.findByRole('heading', { name: 'Settings' });
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('goald is not answering');
    expect(screen.getByRole('heading', { name: 'General' })).toBeTruthy();
  });

  it('keeps Add people and the role change control off for a caller who cannot manage members', async () => {
    const people = await renderPeople({ can: [] });

    expect((within(people).getByRole('button', { name: 'Add people' }) as HTMLButtonElement).disabled).toBe(true);
    expect(within(people).queryByRole('button', { name: 'Change role' })).toBeNull();
  });

  it('gives a member a role on the factory through Add people', async () => {
    const people = await renderPeople({ can: manage, members: [...roster, member('Lee Park', 'member')] });

    fireEvent.click(within(people).getByRole('button', { name: 'Add people' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add people' });
    fireEvent.change(within(dialog).getByLabelText('Person'), { target: { value: 'u-Lee Park' } });
    fireEvent.change(within(dialog).getByLabelText('Role'), { target: { value: 'approver' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add' }));

    await waitFor(() => expect(setFactoryRole).toHaveBeenCalledWith('org-1', 'u-Lee Park', 'factory-1', 'approver'));
  });

  it('does not offer owners or admins in Add people, since they hold every role', async () => {
    const people = await renderPeople({
      can: manage,
      members: [member('Freddy Cruz', 'owner'), member('Lee Park', 'member')],
    });

    fireEvent.click(within(people).getByRole('button', { name: 'Add people' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add people' });
    const person = within(dialog).getByLabelText('Person') as HTMLSelectElement;
    expect([...person.options].map((o) => o.textContent)).toEqual(['Lee Park']);
  });

  it('changes a member’s role through Change role', async () => {
    const people = await renderPeople({ can: manage });

    const row = within(people).getByText('Jon Lee').closest('div') as HTMLElement;
    fireEvent.click(within(row).getByRole('button', { name: 'Change role' }));
    const dialog = await screen.findByRole('dialog', { name: 'Jon Lee on SRE' });
    fireEvent.change(within(dialog).getByLabelText('Role'), { target: { value: 'editor' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(setFactoryRole).toHaveBeenCalledWith('org-1', 'u-Jon Lee', 'factory-1', 'editor'));
  });

  it('removes a member’s role on the factory through Change role', async () => {
    const people = await renderPeople({ can: manage });

    const row = within(people).getByText('Jon Lee').closest('div') as HTMLElement;
    fireEvent.click(within(row).getByRole('button', { name: 'Change role' }));
    const dialog = await screen.findByRole('dialog', { name: 'Jon Lee on SRE' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove from factory' }));

    await waitFor(() => expect(setFactoryRole).toHaveBeenCalledWith('org-1', 'u-Jon Lee', 'factory-1', null));
  });

  it('says the change was not made when the re-sign-in is declined', async () => {
    vi.mocked(setFactoryRole).mockRejectedValue(
      new ApiError({ title: 'Forbidden', status: 403, code: 'reauth-required', detail: 'sign in again' } as never),
    );
    const people = await renderPeople({ can: manage });

    const row = within(people).getByText('Jon Lee').closest('div') as HTMLElement;
    fireEvent.click(within(row).getByRole('button', { name: 'Change role' }));
    const dialog = await screen.findByRole('dialog', { name: 'Jon Lee on SRE' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Not changed: this needs a recent sign-in.'));
  });

  it('shows goald’s message when it refuses a role for an owner or admin (409)', async () => {
    vi.mocked(setFactoryRole).mockRejectedValue(
      new ApiError({
        title: 'Conflict',
        status: 409,
        code: 'editor-everywhere',
        detail: 'owners and admins are editors in every factory',
      } as never),
    );
    const people = await renderPeople({ can: manage, members: [...roster, member('Lee Park', 'member')] });

    fireEvent.click(within(people).getByRole('button', { name: 'Add people' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add people' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add' }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('owners and admins are editors in every factory'));
  });
});
