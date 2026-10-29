// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  connectionsQuery,
  factoriesQuery,
  meQuery,
  notificationsQuery,
  orgQuery,
  personalTokensQuery,
} from '../../api/queries';
import type { Connection, Me, NotificationPreferences, Org, OrgSummary } from '../../api/types';
import { AccountProvider } from '../../app/account-menu';
import { OrgContext } from '../../shared/org';
import { TooltipProvider } from '../../ui/tooltip';
import { AccountPage } from './account-page';

const identity = vi.hoisted(() => ({
  updateName: vi.fn(async () => {}),
  signOutEverywhere: vi.fn(async () => {}),
  signOut: vi.fn(async () => {}),
  keepSessionAlive: vi.fn(async () => {}),
}));
vi.mock('../../api/auth', () => identity);

// goald's client at an absolute origin, so requests are Requests fetch can be stubbed for (the app uses a relative base).
vi.mock(import('../../api/client'), async (original) => {
  const { createGoalClient } = await import('@goal/api-client');
  const { sessionHandlers, withSession } = await import('../../api/session');
  return {
    ...(await original()),
    goal: createGoalClient('http://goald.test', { fetch: withSession((request) => fetch(request), sessionHandlers) }),
  };
});

const toast = vi.hoisted(() => ({ success: vi.fn() }));
vi.mock('sonner', () => ({ toast }));

const org: OrgSummary = { id: 'org-goal', slug: 'goal', name: 'Goal', mark: 'Go', factory_count: 1, role: 'owner' };

const session = {
  kind: 'user',
  mode: 'session',
  platform_admin: false,
  user: { id: 'user-1', name: 'Freddy Cruz', email: 'freddy@example.com' },
} as Me;

const local = { kind: 'user', mode: 'none', platform_admin: false } as Me;

/** The person's saved preferences: the picture's boxes, and the notification email. */
const saved: NotificationPreferences = {
  events: {
    approval: { in_goal: true, email: false, slack: true },
    question: { in_goal: true, email: false, slack: true },
    andon: { in_goal: true, email: true, slack: true },
    budget: { in_goal: true, email: true, slack: false },
    digest: { in_goal: false, email: true, slack: false },
  },
  email: 'freddy.home@example.com',
  time_zone: 'Europe/Berlin',
};

const slackConnection = { id: 'c-slack', name: 'slack', kind: 'slack' } as Connection;

/** The requests the client sent, as the stub below saw them: the method, the path and a JSON body. */
const requests: { method: string; path: string; body: unknown }[] = [];

/** Answers the reads the account page makes as goald does, and a PUT with the body it saved. */
async function answer(input: unknown, init?: RequestInit): Promise<Response> {
  const req = input instanceof Request ? input : undefined;
  const method = req?.method ?? init?.method ?? 'GET';
  const path = new URL(req?.url ?? String(input), 'http://goald.test').pathname;
  const body = req
    ? await req
        .clone()
        .json()
        .catch(() => undefined)
    : typeof init?.body === 'string'
      ? JSON.parse(init.body)
      : undefined;
  requests.push({ method, path, body });
  const json = (status: number, value: unknown, type = 'application/json') =>
    new Response(JSON.stringify(value), { status, headers: { 'content-type': type } });
  if (method === 'PUT') return json(200, body);
  if (path === '/v1/me') return json(200, session);
  if (path === '/v1/me/notifications') return json(200, saved);
  return json(404, { title: 'Not found', status: 404 }, 'application/problem+json');
}

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

/** The account page inside the organization's shell, with the person's state as `me`. */
function renderAccount(
  me: Me | 'pending',
  { prefs = saved, slack = [slackConnection] }: { prefs?: NotificationPreferences; slack?: Connection[] } = {},
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const grants = { ...org, created_at: '2026-09-01T00:00:00Z', can: ['personal_token:read'] } as Org;
  client.setQueryData(orgQuery(org.id).queryKey, grants);
  client.setQueryData(factoriesQuery(org.id).queryKey, []);
  client.setQueryData(personalTokensQuery(org.id).queryKey, []);
  client.setQueryData(notificationsQuery.queryKey, prefs);
  client.setQueryData(connectionsQuery(org.id, { kind: 'slack' }).queryKey, slack);
  if (me === 'pending') vi.spyOn(globalThis, 'fetch').mockReturnValue(new Promise(() => {}));
  else client.setQueryData(meQuery.queryKey, me);

  const rootRoute = createRootRoute();
  const accountRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/account',
    component: () => (
      <OrgContext value={org}>
        <AccountProvider>
          <AccountPage />
        </AccountProvider>
      </OrgContext>
    ),
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([accountRoute]),
    history: createMemoryHistory({ initialEntries: ['/account'] }),
  });

  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  requests.length = 0;
  vi.spyOn(globalThis, 'fetch').mockImplementation(answer);
  stubMatchMedia();
  identity.updateName.mockClear();
  identity.signOutEverywhere.mockClear();
  toast.success.mockClear();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('the account page', () => {
  it('names the page and lists its sections in the picture’s order', async () => {
    renderAccount(session);
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toBeTruthy();
    const nav = screen.getByRole('navigation', { name: 'Account sections' });
    expect(
      within(nav)
        .getAllByRole('link')
        .map((a) => a.textContent),
    ).toEqual(['Profile', 'Appearance', 'Notifications', 'Personal tokens', 'Connected apps', 'Sessions']);
    const headings = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent);
    expect(headings).toEqual([
      'Profile',
      'Appearance',
      'Notifications',
      'Personal tokens',
      'Connected apps',
      'Sessions',
    ]);
  });

  it('starts the name with the person’s name and shows their role', async () => {
    renderAccount(session);
    expect((await screen.findByRole('textbox', { name: 'Name' })).getAttribute('value')).toBe('Freddy Cruz');
    expect(screen.getByText('Owner of Goal')).toBeTruthy();
  });

  it('refuses a name outside 1 to 80 characters and sends nothing', async () => {
    renderAccount(session);
    const name = await screen.findByRole('textbox', { name: 'Name' });
    fireEvent.change(name, { target: { value: '   ' } });
    fireEvent.submit(name.closest('form') as HTMLFormElement);
    expect(await screen.findByText('A name is 1 to 80 characters.')).toBeTruthy();
    expect(identity.updateName).not.toHaveBeenCalled();
  });

  it('saves the trimmed name and says so', async () => {
    renderAccount(session);
    const name = await screen.findByRole('textbox', { name: 'Name' });
    fireEvent.change(name, { target: { value: '  Freddy  ' } });
    fireEvent.submit(name.closest('form') as HTMLFormElement);
    await waitFor(() => expect(identity.updateName).toHaveBeenCalledWith('Freddy'));
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('Name saved'));
  });

  it('shows the saved notification email, and the sign-in email as its placeholder when blank', async () => {
    renderAccount(session);
    const email = await screen.findByRole('textbox', { name: 'Email for notifications' });
    expect((email as HTMLInputElement).value).toBe('freddy.home@example.com');
    expect(email.hasAttribute('disabled')).toBe(false);
  });

  it('saves a changed notification email with the whole preferences, and says so', async () => {
    renderAccount(session);
    const email = await screen.findByRole('textbox', { name: 'Email for notifications' });
    fireEvent.change(email, { target: { value: 'freddy@example.com' } });
    fireEvent.submit(email.closest('form') as HTMLFormElement);
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('Email for notifications saved'));
    expect(requests.find((r) => r.method === 'PUT')?.body).toEqual({ ...saved, email: 'freddy@example.com' });
  });

  it('shows the field error when goald refuses the notification email', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ title: 'Bad', status: 400, code: 'invalid-notification-email' }), {
        status: 400,
        headers: { 'content-type': 'application/problem+json' },
      }),
    );
    renderAccount(session);
    const email = await screen.findByRole('textbox', { name: 'Email for notifications' });
    fireEvent.change(email, { target: { value: 'not an address' } });
    fireEvent.submit(email.closest('form') as HTMLFormElement);
    expect(
      await screen.findByText('Enter one email address, or leave it blank to use your sign-in email.'),
    ).toBeTruthy();
  });

  it('shows the sign-in email as the placeholder while the saved email is blank', async () => {
    renderAccount(session, { prefs: { ...saved, email: '' } });
    const email = await screen.findByRole('textbox', { name: 'Email for notifications' });
    expect(email.getAttribute('placeholder')).toBe('freddy@example.com');
    expect((email as HTMLInputElement).value).toBe('');
  });

  it('keeps the sign-out of another browser disabled, not available yet', async () => {
    renderAccount(session);
    const signOut = await screen.findByRole('button', { name: 'Sign out' });
    expect(signOut.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(signOut);
    expect(identity.signOut).not.toHaveBeenCalled();
    expect(identity.signOutEverywhere).not.toHaveBeenCalled();
  });

  it('opens the sign-out-everywhere confirmation and signs out everywhere once confirmed', async () => {
    renderAccount(session);
    fireEvent.click(await screen.findByRole('button', { name: 'Sign out everywhere' }));
    const dialog = await screen.findByRole('dialog', { name: 'Sign out of every browser and the CLI?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Sign out everywhere' }));
    await waitFor(() => expect(identity.signOutEverywhere).toHaveBeenCalled());
  });

  it('names the local user with no name form or sign-out when sign-in is off', async () => {
    renderAccount(local);
    expect(await screen.findByText('Local user')).toBeTruthy();
    expect(screen.queryByRole('textbox', { name: 'Name' })).toBeNull();
    expect(screen.queryByRole('textbox', { name: 'Email for notifications' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Sign out everywhere' })).toBeNull();
  });

  it('shows Loading while the person loads', async () => {
    renderAccount('pending');
    const busy = await screen.findByText('Loading');
    expect(busy.closest('[aria-busy="true"]')).toBeTruthy();
  });
});
