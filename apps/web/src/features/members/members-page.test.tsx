// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, clearSignupDomain, setFactoryRole, setSignupDomain } from '../../api/client';
import {
  authConfigQuery,
  factoriesQuery,
  invitationsQuery,
  membersQuery,
  meQuery,
  orgQuery,
  settingsQuery,
} from '../../api/queries';
import type { Factory, Invitation, Member, Org, OrgSettings, OrgSummary, SignupDomain } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { MembersPage } from './members-page';

/** The settings the mocked API serves: a failure or a hang makes the switch's query fail or wait. */
const served = vi.hoisted(() => ({
  settings: undefined as OrgSettings | undefined,
  fail: false,
  hang: false,
}));

vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  setFactoryRole: vi.fn(async () => undefined),
  getOrgSettings: vi.fn(async () => {
    if (served.fail) throw new TypeError('Failed to fetch');
    if (served.hang) return new Promise<OrgSettings>(() => {});
    return served.settings;
  }),
  setSignupDomain: vi.fn(
    async (_org: string, body: { domain: string; join_role?: 'viewer' | 'approver' | 'editor' }) => {
      const set: SignupDomain = {
        domain: body.domain,
        join_role: body.join_role ?? 'viewer',
        set_by: { kind: 'user', id: 'u1', name: 'Freddy Cruz' },
        set_at: '2026-10-28T09:00:00Z',
      };
      served.settings = { ...served.settings, signup_domain: set } as OrgSettings;
      return set;
    },
  ),
  clearSignupDomain: vi.fn(async () => {
    const { signup_domain: _cleared, ...rest } = served.settings as OrgSettings;
    served.settings = rest;
  }),
}));

const org: OrgSummary = { id: 'org-acme', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 3, role: 'owner' };

const factories = [
  { id: 'f-goal', name: 'goal' },
  { id: 'f-sre', name: 'sre' },
  { id: 'f-support', name: 'support' },
] as Factory[];

const person = (id: string, name: string, email: string) => ({ id, name, email });

const member = (
  id: string,
  who: ReturnType<typeof person>,
  role: Member['role'],
  factoryRoles: Member['factory_roles'] = [],
  extra: Partial<Member> = {},
): Member => ({
  member_id: `member-${id}`,
  user: who,
  role,
  factory_roles: factoryRoles,
  joined_at: '2026-09-01T00:00:00Z',
  last_active_at: '2026-10-26T12:00:00Z',
  you: false,
  can_change_role: false,
  can_remove: false,
  ...extra,
});

/** Four people: the owner (you), an admin, a member in two factories and a member in one. */
const seedMembers: Member[] = [
  member('1', person('u1', 'Freddy Cruz', 'freddy@example.com'), 'owner', [], { you: true }),
  member('2', person('u2', 'Maya Ruiz', 'maya@example.com'), 'admin'),
  member(
    '3',
    person('u3', 'Jonas Okafor', 'jonas@example.com'),
    'member',
    [
      { factory_id: 'f-goal', role: 'editor' },
      { factory_id: 'f-sre', role: 'editor' },
    ],
    { can_change_role: true, can_remove: true },
  ),
  member('4', person('u4', 'Lena Park', 'lena@example.com'), 'member', [{ factory_id: 'f-support', role: 'viewer' }], {
    can_change_role: true,
    can_remove: true,
  }),
];

const invitation = (id: string, email: string, factoryRoles: Invitation['factory_roles']): Invitation =>
  ({
    id,
    email,
    role: 'member',
    factory_roles: factoryRoles,
    created_at: '2026-10-24T09:00:00Z',
    expires_at: '2026-10-31T09:00:00Z',
    inviter: { id: 'u2', name: 'Maya Ruiz' },
  }) as Invitation;

const seedInvitations: Invitation[] = [
  invitation('i1', 'sam@example.com', [{ factory_id: 'f-sre', role: 'editor' }]),
  invitation('i2', 'ops@example.com', [{ factory_id: 'f-support', role: 'viewer' }]),
];

interface Setup {
  /** The organization role the caller holds. */
  role?: 'owner' | 'admin' | 'member';
  /** Sign-in on: owners and admins can manage the organization. */
  mode?: 'session' | 'local';
  members?: Member[];
  invitations?: Invitation[];
  state?: 'ready' | 'loading' | 'error';
  /** The sign-up domain goald holds, if any; the settings' own states are `settings`. */
  signupDomain?: SignupDomain;
  settings?: 'ready' | 'loading' | 'error';
}

function renderMembers({
  role = 'owner',
  mode = 'session',
  members = seedMembers,
  invitations = seedInvitations,
  state = 'ready',
  signupDomain,
  settings = 'ready',
}: Setup = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const detail = {
    ...org,
    created_at: '2026-09-01T00:00:00Z',
    role,
    can:
      role === 'member'
        ? ['member:read', 'org:read']
        : ['member:manage', 'member:read', 'org:read', 'invitation:read', 'invitation:manage'],
  } as Org;
  client.setQueryData(orgQuery(org.id).queryKey, detail);
  client.setQueryData(authConfigQuery.queryKey, { mode } as never);
  client.setQueryData(factoriesQuery(org.id).queryKey, factories);
  client.setQueryData(invitationsQuery(org.id).queryKey, invitations);
  if (state === 'ready') client.setQueryData(membersQuery(org.id).queryKey, members);
  client.setQueryData(meQuery.queryKey, {
    kind: 'user',
    mode: 'session',
    user: { id: 'u1', name: 'Freddy Cruz', email: 'freddy@example.com' },
    platform_admin: false,
  });
  served.fail = settings === 'error';
  served.hang = settings === 'loading';
  served.settings = {
    name: org.name,
    slug: org.slug,
    mark: org.mark,
    slots: { max: null, factories: [] },
    ...(signupDomain ? { signup_domain: signupDomain } : {}),
  };
  if (settings === 'ready') client.setQueryData(settingsQuery(org.id).queryKey, served.settings);
  if (state === 'loading') vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}));
  if (state === 'error') vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Failed to fetch'));

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org }),
    component: () => (
      <>
        <MembersPage />
        <Outlet />
      </>
    ),
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([orgRoute]),
    history: createMemoryHistory({ initialEntries: ['/o/acme'] }),
  });

  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

/** jsdom has no matchMedia, which the theme reads to follow the system. */
function stubMatchMedia() {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
}

const sign = (domain: string): SignupDomain => ({
  domain,
  join_role: 'viewer',
  set_by: { kind: 'user', id: 'u1', name: 'Freddy Cruz' },
  set_at: '2026-10-20T09:00:00Z',
});

const membersCard = () => screen.getByRole('region', { name: 'Members' });

beforeEach(stubMatchMedia);

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.mocked(setFactoryRole).mockClear();
  vi.mocked(setSignupDomain).mockClear();
  vi.mocked(clearSignupDomain).mockClear();
});

describe('members and roles, ready', () => {
  it('shows the count, the members table and the invitations waiting to join', async () => {
    renderMembers();
    expect(await screen.findByText('4 members, 2 invites waiting')).toBeTruthy();

    const table = within(membersCard()).getByRole('table');
    for (const column of ['Name', 'Role', 'Factories', 'Last active']) {
      expect(within(table).getByRole('columnheader', { name: column })).toBeTruthy();
    }
    expect(within(table).getByRole('row', { name: /Freddy Cruz/ }).textContent).toContain('Owner');
    expect(within(table).getByRole('row', { name: /Jonas Okafor/ }).textContent).toContain('goal editor, sre editor');

    const waiting = screen.getByRole('region', { name: /Waiting to join/ });
    expect(within(waiting).getAllByText('sam@example.com').length).toBeGreaterThan(0);
    expect(within(waiting).getAllByRole('button', { name: 'Resend the invitation to ops@example.com' })).toHaveLength(
      2,
    );
    expect(within(waiting).getAllByRole('button', { name: 'Revoke the invitation to ops@example.com' })).toHaveLength(
      2,
    );
  });

  it('keeps the invite link controls disabled, says why on focus, and runs nothing', async () => {
    renderMembers();
    const copy = await screen.findByRole('button', { name: 'Copy' });
    expect(copy?.getAttribute('aria-disabled')).toBe('true');
    copy.focus();
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
    fireEvent.click(copy);

    expect(setFactoryRole).not.toHaveBeenCalled();
    expect(setSignupDomain).not.toHaveBeenCalled();
  });

  it('lists the organization role matrix with a mark for each yes and no', async () => {
    renderMembers();
    const heading = await screen.findByRole('heading', { name: 'What each role can do' });
    const matrix = within(heading.closest('section') as HTMLElement).getByRole('table');
    expect(within(matrix).getByRole('columnheader', { name: 'Owner' })).toBeTruthy();
    expect(within(matrix).getByRole('columnheader', { name: 'Member' })).toBeTruthy();
    const transfer = within(matrix).getByRole('row', { name: /Transfer or delete the organization/ });
    expect(within(transfer).getAllByText('Yes')).toHaveLength(1);
    expect(within(transfer).getAllByText('No')).toHaveLength(2);
    const definitions = within(matrix).getByRole('row', { name: /Edit and publish definitions/ });
    expect(within(definitions).getAllByText('Yes')).toHaveLength(3);
    expect(within(definitions).queryAllByText('No')).toHaveLength(0);
  });

  it('changes a factory role from the person’s sheet, at once', async () => {
    renderMembers();
    fireEvent.pointerDown(await screen.findByRole('button', { name: 'More for Jonas Okafor' }), {
      button: 0,
      ctrlKey: false,
      pointerType: 'mouse',
    });
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Factory roles…' }));
    const dialog = await screen.findByRole('dialog', { name: 'Jonas Okafor' });
    const goal = within(dialog).getByRole('group', { name: 'goal: role of Jonas Okafor' });
    fireEvent.click(within(goal).getByRole('radio', { name: 'Viewer' }));
    await waitFor(() => expect(setFactoryRole).toHaveBeenCalledWith(org.id, 'u3', 'f-goal', 'viewer'));
  });

  it('hides invitation and invite link controls from a member', async () => {
    renderMembers({
      role: 'member',
      invitations: [],
      members: seedMembers.map((m) => ({ ...m, you: m.user.id === 'u3' })),
    });
    expect(await screen.findByText('4 members')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /^Invite/ })).toBeNull();
    expect(screen.queryByRole('region', { name: 'Invite link' })).toBeNull();
    expect(screen.queryByRole('region', { name: /Waiting to join/ })).toBeNull();
  });
});

describe('sign-up by domain', () => {
  const sw = () => screen.findByRole('switch', { name: 'Restrict sign-up to a domain' });
  const checked = () =>
    screen.getByRole('switch', { name: 'Restrict sign-up to a domain' }).getAttribute('aria-checked');

  it('turns the switch on for the owner’s email domain, joining as a viewer', async () => {
    renderMembers();
    const domain = await sw();
    expect(domain.getAttribute('aria-disabled')).toBeNull();
    expect(domain.getAttribute('aria-checked')).toBe('false');
    expect(screen.getByText('Anyone at example.com joins as a Viewer')).toBeTruthy();

    fireEvent.click(domain);
    await waitFor(() => expect(checked()).toBe('true'));
    expect(setSignupDomain).toHaveBeenCalledWith('org-acme', { domain: 'example.com', join_role: 'viewer' });
  });

  it('turns the switch off, which clears the domain', async () => {
    renderMembers({ signupDomain: sign('example.com') });
    const domain = await sw();
    expect(domain.getAttribute('aria-checked')).toBe('true');

    fireEvent.click(domain);
    await waitFor(() => expect(checked()).toBe('false'));
    expect(clearSignupDomain).toHaveBeenCalledWith('org-acme');
  });

  it('shows a refusal under the switch and leaves the switch as it was', async () => {
    vi.mocked(setSignupDomain).mockRejectedValueOnce(
      new ApiError({ title: 'Bad Request', status: 400, code: 'signup-domain-not-yours' } as never),
    );
    renderMembers();
    fireEvent.click(await sw());

    expect((await screen.findByRole('alert')).textContent).toContain('Your sign-in email is not at that domain');
    expect(checked()).toBe('false');
  });

  it('shows a public email provider and a domain another organization holds as refusals', async () => {
    vi.mocked(setSignupDomain).mockRejectedValueOnce(
      new ApiError({ title: 'Bad Request', status: 400, code: 'signup-domain-public' } as never),
    );
    renderMembers();
    fireEvent.click(await sw());
    expect((await screen.findByRole('alert')).textContent).toContain(
      'A public email provider cannot be a sign-up domain',
    );
  });

  it('shows an admin the switch disabled, as stored, and sends nothing', async () => {
    renderMembers({ role: 'admin', signupDomain: sign('example.com') });
    const domain = await sw();
    expect(domain.getAttribute('aria-disabled')).toBe('true');
    expect(domain.getAttribute('aria-checked')).toBe('true');
    fireEvent.click(domain);
    expect(setSignupDomain).not.toHaveBeenCalled();
    expect(clearSignupDomain).not.toHaveBeenCalled();
  });

  it('shows a member the switch disabled and as stored, under a Sign-up card', async () => {
    renderMembers({ role: 'member', invitations: [], signupDomain: sign('example.com') });
    const domain = await sw();
    expect(domain.getAttribute('aria-disabled')).toBe('true');
    expect(domain.getAttribute('aria-checked')).toBe('true');
    expect(screen.getByRole('region', { name: 'Sign-up' })).toBeTruthy();
    fireEvent.click(domain);
    expect(clearSignupDomain).not.toHaveBeenCalled();
  });

  it('shows the switch busy while the settings load', async () => {
    renderMembers({ settings: 'loading' });
    const domain = await sw();
    expect(domain.getAttribute('aria-busy')).toBe('true');
    expect(screen.getByText('Loading', { selector: 'span.sr-only' })).toBeTruthy();
  });

  it('says the switch could not load when the settings fail', async () => {
    renderMembers({ settings: 'error' });
    expect((await screen.findByRole('alert')).textContent).toContain('Sign-up by domain could not load.');
  });
});

describe('members and roles, other states', () => {
  it('says nobody matches a search that finds no one', async () => {
    renderMembers();
    fireEvent.change(await screen.findByRole('searchbox', { name: 'Search members' }), { target: { value: 'zzz' } });
    expect(screen.getByText('Nobody matches “zzz”.')).toBeTruthy();
  });

  it('shows the loading state as busy with a screen-reader label', async () => {
    renderMembers({ state: 'loading' });
    const label = await screen.findByText('Loading');
    expect(label.closest('[aria-busy]')?.getAttribute('aria-busy')).toBe('true');
  });

  it('says the page could not load when the members fail', async () => {
    renderMembers({ state: 'error' });
    expect((await screen.findByRole('alert')).textContent).toContain('Members could not load.');
  });
});
