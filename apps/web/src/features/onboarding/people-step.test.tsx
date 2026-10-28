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
import { invite } from '../../api/auth';
import { listInvitations, listMembers } from '../../api/client';
import { getFactory, getVersionModel } from '../../api/definitions';
import { getIntake } from '../../api/intake';
import type { Factory, Intake, Invitation, Member, OrgSummary } from '../../api/types';
import { OrgContext } from '../../shared/org';
import { TooltipProvider } from '../../ui/tooltip';
import { onboardingSearch } from './code-search';
import { PeopleStep } from './people-step';

vi.mock('../../api/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/auth')>()),
  invite: vi.fn(),
}));
vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  listMembers: vi.fn(),
  listInvitations: vi.fn(),
}));
vi.mock('../../api/definitions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/definitions')>()),
  getFactory: vi.fn(),
  getVersionModel: vi.fn(),
}));
vi.mock('../../api/intake', () => ({ getIntake: vi.fn() }));

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factoryId = '6f1d2c3a-4b5e-4f70-8a9b-0c1d2e3f4a5b';

const you: Member = {
  member_id: 'm-ada',
  user: { id: 'u-ada', name: 'Ada Lovelace', email: 'ada@example.com' },
  role: 'owner',
  factory_roles: [],
  joined_at: '2026-10-01T10:00:00Z',
  last_active_at: null,
  you: true,
  can_change_role: false,
  can_remove: false,
} as Member;

/** A pending invitation for `email`; the list holds only the pending ones. */
function invitation(email: string, over: Partial<Invitation> = {}): Invitation {
  return {
    id: `inv-${email}`,
    email,
    role: 'member',
    factory_roles: [{ factory_id: factoryId, role: 'viewer' }],
    inviter: { id: 'u-ada', name: 'Ada Lovelace' },
    created_at: '2026-10-27T10:00:00Z',
    expires_at: '2026-10-30T10:00:00Z',
    ...over,
  } as Invitation;
}

let invitations: Invitation[];

beforeEach(() => {
  invitations = [invitation('sam@example.com')];
  vi.mocked(getFactory).mockResolvedValue({
    id: factoryId,
    name: 'code-review',
    title: 'Code review',
    current: { id: 'version-1', number: 1 },
    draft: null,
  } as unknown as Factory);
  vi.mocked(getVersionModel).mockResolvedValue({
    model: { basics: { name: 'code-review', purpose: 'Reviews pull requests.' }, line: { stations: [] }, review: {} },
  } as never);
  vi.mocked(getIntake).mockResolvedValue({
    items: 0,
    from: null,
    to: null,
    sources: { github: 0, people: 0, cli: 0, api: 0, schedule: 0, webhook: 0 },
    triggers: [],
  } as Intake);
  vi.mocked(listMembers).mockResolvedValue([you]);
  // The invitations list answers what has been sent so far, as the server does.
  vi.mocked(listInvitations).mockImplementation(async () => invitations);
  vi.mocked(invite).mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

/** Step 7 at /o/acme/get-started/people with the URL's search; the step it goes to is a stub that names it. */
function renderStep(url: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: Outlet });
  const step = createRoute({
    getParentRoute: () => root,
    path: '/o/$org/get-started/$step',
    validateSearch: (search: Record<string, unknown>) => onboardingSearch(search),
    component: function Page() {
      const search = step.useSearch();
      const { step: key } = step.useParams();
      if (key === 'people') return <PeopleStep org={org} search={search} />;
      return <p>{`Step ${key}`}</p>;
    },
  });
  const router = createRouter({
    routeTree: root.addChildren([step]),
    history: createMemoryHistory({ initialEntries: [url] }),
  });
  render(
    <QueryClientProvider client={queryClient}>
      <OrgContext value={org}>
        <TooltipProvider>
          <RouterProvider router={router} />
        </TooltipProvider>
      </OrgContext>
    </QueryClientProvider>,
  );
  return { router, queryClient };
}

const url = `/o/acme/get-started/people?factory=${factoryId}`;

/** Adds one address with the role the select holds (Builder unless changed). */
function addAddress(email: string) {
  fireEvent.change(screen.getByLabelText('Email'), { target: { value: email } });
  fireEvent.click(screen.getByRole('button', { name: 'Add' }));
}

describe('step 7, invite people', () => {
  it('sends a Builder as a member with the editor role on the factory, then goes to step 8', async () => {
    renderStep(url);
    await screen.findByRole('heading', { level: 1, name: 'Invite people' });
    expect((screen.getByLabelText('Role') as HTMLSelectElement).value).toBe('builder');

    addAddress('Alex@Example.com');
    const list = screen.getByRole('list', { name: 'People to invite' });
    expect(within(list).getByText('alex@example.com')).toBeTruthy();
    expect(within(list).getByText('Builder')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Send 1 invite' }));

    await waitFor(() =>
      expect(invite).toHaveBeenCalledWith({
        organizationId: 'org-1',
        email: 'alex@example.com',
        role: 'member',
        factoryRoles: [{ factoryId, role: 'editor' }],
      }),
    );
    expect(await screen.findByText('Step first-item')).toBeTruthy();
  });

  it('names a refused address and keeps it in the list, and sends nothing on to step 8', async () => {
    vi.mocked(invite).mockImplementation(async (request) => {
      if (request.email === 'alex@example.com') {
        throw Object.assign(new Error('already'), { kind: 'already-member' });
      }
    });
    renderStep(url);
    await screen.findByRole('heading', { level: 1, name: 'Invite people' });

    addAddress('alex@example.com');
    fireEvent.click(screen.getByRole('button', { name: 'Send 1 invite' }));

    expect(await screen.findByText('Not sent to alex@example.com: Already a member of Acme.')).toBeTruthy();
    const list = screen.getByRole('list', { name: 'People to invite' });
    expect(within(list).getByText('alex@example.com')).toBeTruthy();
    expect(screen.queryByText('Step first-item')).toBeNull();
  });

  it('skips the invitations and goes to step 8 without sending', async () => {
    renderStep(url);
    await screen.findByRole('heading', { level: 1, name: 'Invite people' });

    fireEvent.click(screen.getByRole('link', { name: 'Skip for now' }));

    expect(await screen.findByText('Step first-item')).toBeTruthy();
    expect(invite).not.toHaveBeenCalled();
  });

  it('says a role description for each of the four roles, and lists the caller and the pending invitations in the panel', async () => {
    renderStep(url);
    await screen.findByRole('heading', { level: 1, name: 'Invite people' });

    expect(screen.getByText('Everything, including billing')).toBeTruthy();
    expect(screen.getByText('Factories, people and connections')).toBeTruthy();
    expect(screen.getByText('Definitions, items and approvals')).toBeTruthy();
    expect(screen.getByText('Sees everything, changes nothing')).toBeTruthy();

    const panel = await screen.findByRole('region', { name: 'People' });
    expect(within(panel).getByText('You, owner')).toBeTruthy();
    expect(within(panel).getByText('sam, viewer, invited')).toBeTruthy();
  });

  it('names a refused address without adding a second row for it', async () => {
    renderStep(url);
    await screen.findByRole('heading', { level: 1, name: 'Invite people' });

    addAddress('alex@example.com');
    addAddress('alex@example.com');
    const list = screen.getByRole('list', { name: 'People to invite' });
    expect(within(list).getAllByRole('listitem')).toHaveLength(1);
  });

  it('shows loading while the factory is read', async () => {
    vi.mocked(getFactory).mockReturnValue(new Promise(() => {}));
    renderStep(url);
    expect((await screen.findByRole('status')).textContent).toBe('Loading');
  });

  it('shows the error state with Retry when the model cannot be read', async () => {
    vi.mocked(getVersionModel).mockRejectedValue(new Error('down'));
    renderStep(url);
    expect(await screen.findByText('The factory can’t be read.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy();
  });
});
