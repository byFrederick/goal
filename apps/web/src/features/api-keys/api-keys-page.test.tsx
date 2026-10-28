// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { type ReactNode, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { apiKeysQuery, factoriesQuery, memberTokensQuery, orgQuery } from '../../api/queries';
import type { ApiKey, Factory, Org, OrgSummary } from '../../api/types';
import { TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { OrgContext } from '../../shared/org';
import { TooltipProvider } from '../../ui/tooltip';
import { ApiKeysPage } from './api-keys-page';

const org: OrgSummary = { id: 'org-acme', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 1, role: 'owner' };
const granted = { ...org, created_at: '2026-09-01T00:00:00Z', can: ['api_key:read', 'api_key:manage'] } as Org;
const cruz = { kind: 'user', id: 'user-1', name: 'Freddy Cruz' } as ApiKey['created_by'];
const day = 86_400_000;
const now = Date.now();

function key(over: Partial<ApiKey> & Pick<ApiKey, 'id' | 'name'>): ApiKey {
  return {
    hint: 'goal_sk_Ab3x',
    scopes: ['item:create'],
    role_ceiling: 'approver',
    factories: null,
    created_by: cruz,
    created_at: new Date(now - 30 * day).toISOString(),
    expires_at: new Date(now + 60 * day).toISOString(),
    last_used_at: new Date(now - 14 * 60_000).toISOString(),
    revoked_at: null,
    revoked_by: null,
    ...over,
  } as ApiKey;
}

const pictured: ApiKey[] = [
  key({ id: 'k1', name: 'ci-items' }),
  key({
    id: 'k2',
    name: 'grafana-metrics',
    hint: 'goal_sk_Q7mz',
    scopes: ['factory:read', 'org:read'],
    role_ceiling: 'viewer',
    last_used_at: new Date(now - 2 * 3_600_000).toISOString(),
  }),
  key({
    id: 'k3',
    name: 'deploy-bot',
    hint: 'goal_sk_t0Rk',
    scopes: ['item:create', 'item:operate'],
    factories: ['f-software'],
    last_used_at: null,
    expires_at: new Date(now + 85 * day).toISOString(),
  }),
];

/** The page inside the organization's context, with the top bar's actions shown beside it as the shell shows them. */
async function renderPage(keys: ApiKey[] | 'pending' | 'error') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(orgQuery(org.id).queryKey, granted);
  client.setQueryData(factoriesQuery(org.id).queryKey, [{ id: 'f-software', name: 'Software' }] as Factory[]);
  client.setQueryData(memberTokensQuery(org.id).queryKey, []);
  if (keys === 'pending') vi.spyOn(globalThis, 'fetch').mockReturnValue(new Promise(() => {}));
  else if (keys === 'error')
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ title: 'Server error', status: 500 }), {
        status: 500,
        headers: { 'Content-Type': 'application/problem+json' },
      }),
    );
  else client.setQueryData(apiKeysQuery(org.id).queryKey, keys);

  const rootRoute = createRootRoute();
  const apiKeysRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org/api-keys',
    component: () => (
      <Shell>
        <ApiKeysPage />
      </Shell>
    ),
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([apiKeysRoute]),
    history: createMemoryHistory({ initialEntries: ['/o/acme/api-keys'] }),
  });

  await router.load();
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <OrgContext value={org}>
          <RouterProvider router={router} />
        </OrgContext>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

/** Stands in for the shell's top bar: it shows the actions a page sets with TopBarSlot. */
function Shell({ children }: { children: ReactNode }) {
  const [top, setTop] = useState<TopBarState | null>(null);
  return (
    <TopBarSetContext value={setTop}>
      <div>{top?.actions}</div>
      {children}
    </TopBarSetContext>
  );
}

beforeEach(() => {
  vi.spyOn(globalThis, 'fetch').mockReturnValue(new Promise(() => {}));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('API keys page', () => {
  it('shows the heading, the description and the three keys as pictured', async () => {
    await renderPage(pictured);

    expect(screen.getByRole('heading', { level: 1, name: 'API keys' })).toBeTruthy();
    expect(
      screen.getByText('For scripts and services that use Goal’s API as the organization, not as a person.'),
    ).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Keys' })).toBeTruthy();

    const table = screen.getByRole('table', { name: 'API keys of Acme' });
    for (const name of ['ci-items', 'grafana-metrics', 'deploy-bot']) {
      expect(within(table).getByText(name)).toBeTruthy();
    }
    const deploy = within(table).getByText('deploy-bot').closest('tr');
    expect(within(deploy as HTMLElement).getByText('Software')).toBeTruthy();
    expect(within(deploy as HTMLElement).getByRole('button', { name: 'Revoke deploy-bot' })).toBeTruthy();
  });

  it('shows the terminal sign-in and the agents card, with the MCP address disabled', async () => {
    await renderPage(pictured);

    expect(screen.getByRole('heading', { name: 'From your terminal' })).toBeTruthy();
    expect(screen.getByText('goal login', { exact: false })).toBeTruthy();
    expect(screen.getByText('goal item create --factory Software', { exact: false })).toBeTruthy();

    expect(screen.getByRole('heading', { name: 'Goal for other agents' })).toBeTruthy();
    expect(screen.getByText(`${window.location.origin}/mcp`)).toBeTruthy();
    const copy = screen.getByRole('button', { name: 'Copy' });
    expect(copy.getAttribute('aria-disabled')).toBe('true');
    copy.focus();
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
  });

  it('keeps New key in the top bar while the list is ready', async () => {
    await renderPage(pictured);
    expect(screen.getByRole('button', { name: 'New key' })).toBeTruthy();
  });

  it('shows a loading status with no placeholder bars while the keys load', async () => {
    await renderPage('pending');

    const status = screen.getByRole('status');
    expect(status.textContent).toBe('Loading');
    expect(status.closest('[aria-busy="true"]')).not.toBeNull();
    expect(screen.queryByRole('heading', { name: 'Keys' })).toBeNull();
    expect(screen.getByRole('heading', { level: 1, name: 'API keys' })).toBeTruthy();
  });

  it('keeps the header and New key when the list fails, and offers to try again', async () => {
    await renderPage('error');

    expect(await screen.findByText('Couldn’t load API keys. Check your connection and try again.')).toBeTruthy();
    expect(screen.getByRole('heading', { level: 1, name: 'API keys' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'New key' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('says there are no keys yet, with one action to make one', async () => {
    await renderPage([]);

    expect(screen.getByRole('heading', { name: 'No keys yet' })).toBeTruthy();
    expect(screen.getByText('Create one for CI or a script.')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: 'New key' }).length).toBeGreaterThan(0);
  });

  it('opens the revoke confirmation from a key’s row', async () => {
    await renderPage(pictured);
    // The table and the phone list both render; jsdom applies no breakpoint, so the first is taken.
    fireEvent.click(screen.getAllByRole('button', { name: 'Revoke ci-items' })[0] as HTMLElement);
    expect(screen.getByRole('dialog', { name: 'Revoke ci-items?' })).toBeTruthy();
  });
});
