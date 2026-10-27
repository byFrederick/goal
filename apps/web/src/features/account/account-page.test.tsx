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
import { factoriesQuery, meQuery, orgQuery, personalTokensQuery } from '../../api/queries';
import type { Me, Org, OrgSummary } from '../../api/types';
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
function renderAccount(me: Me | 'pending') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const grants = { ...org, created_at: '2026-09-01T00:00:00Z', can: ['personal_token:read'] } as Org;
  client.setQueryData(orgQuery(org.id).queryKey, grants);
  client.setQueryData(factoriesQuery(org.id).queryKey, []);
  client.setQueryData(personalTokensQuery(org.id).queryKey, []);
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

  it('keeps the email for notifications disabled, not available yet', async () => {
    renderAccount(session);
    const email = await screen.findByPlaceholderText('you@example.com');
    expect(email.getAttribute('aria-disabled')).toBe('true');
  });

  it('keeps the notification boxes disabled and unchanged', async () => {
    renderAccount(session);
    const box = await screen.findByRole('checkbox', { name: 'An agent asks you a question, Slack' });
    expect(box.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(box);
    expect((box as HTMLInputElement).checked).toBe(false);
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
    expect(screen.queryByRole('button', { name: 'Sign out everywhere' })).toBeNull();
  });

  it('shows Loading while the person loads', async () => {
    renderAccount('pending');
    const busy = await screen.findByText('Loading');
    expect(busy.closest('[aria-busy="true"]')).toBeTruthy();
  });
});
