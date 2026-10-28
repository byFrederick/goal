// @vitest-environment jsdom
import { QueryClient, QueryClientProvider, type QueryKey } from '@tanstack/react-query';
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
import { ApiError } from '../../api/client';
import { createCredential, importCredentials, listCredentials } from '../../api/credentials';
import { factoriesQuery, orgQuery, versionModelQuery } from '../../api/queries';
import type { Factory, Org, OrgSummary } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { NewCredentialPage } from './new-credential-page';

// The page's requests are mocked: the list, the create and the import answer what a test sets. The
// screen mounts under the organization's credentials route, with the factories and each factory's
// agents seeded so nothing else leaves the test.
vi.mock('../../api/credentials', () => ({
  listCredentials: vi.fn(async () => []),
  createCredential: vi.fn(async (_org: string, body: { name: string; type: string }) => ({
    id: 'cred-new',
    name: body.name,
    type: body.type,
  })),
  importCredentials: vi.fn(),
  updateCredential: vi.fn(),
  deleteCredential: vi.fn(),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 2, role: null };
const software = { id: 'f-software', name: 'software', title: 'Software' } as unknown as Factory;
const sre = { id: 'f-sre', name: 'sre', title: 'SRE' } as unknown as Factory;
const model = (...agents: string[]) =>
  ({ model: { agents: { agents: agents.map((name) => ({ name })) } }, ranges: {} }) as never;

beforeEach(() => {
  // jsdom has no scrollTo; the router calls it on navigation.
  window.scrollTo = () => {};
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

/** Renders the New credential page at /o/acme/credentials/new, with `?panel=import` when `panel` is set. */
function renderNew({ panel, can = ['credential:manage'] }: { panel?: 'import'; can?: string[] } = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const grants = { ...org, can, archived_at: null } as unknown as Org;
  const seed: (readonly [QueryKey, unknown])[] = [
    [orgQuery(org.id).queryKey, grants],
    [factoriesQuery(org.id).queryKey, [software, sre]],
    [versionModelQuery(org.id, software.id, 'current').queryKey, model('architect', 'infra-engineer')],
    [versionModelQuery(org.id, sre.id, 'current').queryKey, model('investigator', 'fixer')],
  ];
  for (const [key, data] of seed) queryClient.setQueryData(key, data);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org }),
    component: Outlet,
  });
  const orgPages = createRoute({ getParentRoute: () => orgRoute, id: '_org', component: Outlet });
  const credentials = createRoute({ getParentRoute: () => orgPages, path: 'credentials', component: Outlet });
  const list = createRoute({ getParentRoute: () => credentials, path: '/', component: () => <p>Credentials list</p> });
  const create = createRoute({ getParentRoute: () => credentials, path: 'new', component: NewCredentialPage });
  const routeTree = rootRoute.addChildren([
    orgRoute.addChildren([orgPages.addChildren([credentials.addChildren([list, create])])]),
  ]);

  const router = createRouter({
    routeTree,
    history: createMemoryHistory({
      initialEntries: [`/o/acme/credentials/new${panel ? '?panel=import' : ''}`],
    }),
  });

  return {
    ...render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <RouterProvider router={router} />
        </TooltipProvider>
      </QueryClientProvider>,
    ),
    router,
    queryClient,
  };
}

/** Fills the Create one form with a valid credential scoped to Software, and its architect agent. */
function fillCreate({ name = 'cloudflare-token-2027', value = 'cf_test-value-1234' } = {}) {
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: name } });
  fireEvent.change(screen.getByLabelText('Value'), { target: { value } });
  fireEvent.click(screen.getByRole('button', { name: /Software/ }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'architect' }));
}

describe('creating one credential', () => {
  it('sends the name, type, scope, agents, expiry and reminder, then returns to the list', async () => {
    const { router } = renderNew();
    await screen.findByRole('heading', { name: 'Create one' });

    fillCreate();
    fireEvent.change(screen.getByLabelText('Expires'), { target: { value: '2027-10-29' } });
    fireEvent.change(screen.getByLabelText('Remind me to rotate it'), { target: { value: '7' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create credential' }));

    await waitFor(() =>
      expect(createCredential).toHaveBeenCalledWith(
        'org-1',
        expect.objectContaining({
          name: 'cloudflare-token-2027',
          type: 'api_key',
          value: 'cf_test-value-1234',
          scope: { all_factories: false, factories: ['f-software'] },
          agents: { 'f-software': ['architect'] },
          expires_at: '2027-10-29T00:00:00.000Z',
          remind_days: 7,
        }),
      ),
    );
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/credentials'));
  });

  it('refreshes the credentials list after the create', async () => {
    renderNew();
    await screen.findByRole('heading', { name: 'Create one' });
    await waitFor(() => expect(listCredentials).toHaveBeenCalled());
    const before = vi.mocked(listCredentials).mock.calls.length;

    fillCreate();
    fireEvent.click(screen.getByRole('button', { name: 'Create credential' }));

    await waitFor(() => expect(createCredential).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(vi.mocked(listCredentials).mock.calls.length).toBeGreaterThan(before));
  });

  it('refuses a name that is not a credential name, and sends nothing', async () => {
    renderNew();
    await screen.findByRole('heading', { name: 'Create one' });

    fillCreate({ name: 'Bad_Name' });
    fireEvent.click(screen.getByRole('button', { name: 'Create credential' }));

    expect(await screen.findByText(/Use 3 to 40 lowercase letters/)).toBeTruthy();
    expect(createCredential).not.toHaveBeenCalled();
  });

  it('shows the API’s refusal on the form for a name that is taken', async () => {
    vi.mocked(createCredential).mockRejectedValueOnce(
      new ApiError({
        title: 'Conflict',
        status: 409,
        detail: 'credential "cloudflare-token-2027" already exists',
        code: 'credential-name-taken',
      } as unknown as ConstructorParameters<typeof ApiError>[0]),
    );
    renderNew();
    await screen.findByRole('heading', { name: 'Create one' });

    fillCreate();
    fireEvent.click(screen.getByRole('button', { name: 'Create credential' }));

    expect((await screen.findByRole('alert')).textContent).toContain(
      'credential "cloudflare-token-2027" already exists',
    );
  });

  it('says it replaces nothing when a similar credential exists', async () => {
    const { queryClient } = renderNew();
    await screen.findByRole('heading', { name: 'Create one' });
    queryClient.setQueryData(['org', 'org-1', 'credentials'], [{ name: 'cloudflare-token' }]);

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'cloudflare-token-2027' } });

    expect(
      await screen.findByText('Replaces nothing. The old cloudflare-token keeps working until you remove it.'),
    ).toBeTruthy();
  });

  it('never shows the value after the submit, not in the page, the URL or any cache', async () => {
    const value = 'cf_never-shown-9876';
    const { router, queryClient } = renderNew();
    await screen.findByRole('heading', { name: 'Create one' });

    fillCreate({ value });
    fireEvent.click(screen.getByRole('button', { name: 'Create credential' }));

    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/credentials'));
    expect(document.body.textContent).not.toContain(value);
    expect(router.state.location.href).not.toContain(value);
    for (const query of queryClient.getQueryCache().getAll()) {
      expect(JSON.stringify(query.state.data ?? null)).not.toContain(value);
    }
    expect(
      queryClient
        .getMutationCache()
        .getAll()
        .map((m) => JSON.stringify(m.state)),
    ).not.toContainEqual(expect.stringContaining(value));
  });
});

describe('importing many credentials', () => {
  const yaml = [
    'credentials:',
    '  sentry-token: sntrys_test-1',
    '  linear-key: lin_api_test-2',
    '  cloudflare-token: cf_test-3',
  ].join('\n');

  /** The dry run's answer for the sample: two new names, and the existing one with two connections. */
  const found = {
    dry_run: true,
    credentials: [
      { name: 'sentry-token', exists: false, connections: 0 },
      { name: 'linear-key', exists: false, connections: 0 },
      { name: 'cloudflare-token', exists: true, connections: 2 },
    ],
  };

  it('dry-runs the pasted YAML and lists each name as new or existing', async () => {
    vi.mocked(importCredentials).mockResolvedValue(found);
    renderNew({ panel: 'import' });

    const paste = await screen.findByLabelText('Paste .env or YAML');
    fireEvent.change(paste, { target: { value: yaml } });

    expect(await screen.findByText('3 found')).toBeTruthy();
    expect(importCredentials).toHaveBeenCalledWith('org-1', { format: 'yaml', text: yaml, dry_run: true });
    expect(screen.getAllByText('New')).toHaveLength(2);
    expect(screen.getByText('Already exists')).toBeTruthy();
    expect(screen.getByText(/Used by 2 connections/)).toBeTruthy();
  });

  it('skips the existing name by default, and replaces it when Replace is chosen', async () => {
    vi.mocked(importCredentials).mockResolvedValue(found);
    renderNew({ panel: 'import' });
    fireEvent.change(await screen.findByLabelText('Paste .env or YAML'), { target: { value: yaml } });
    await screen.findByText('3 found');

    const skip = screen.getByRole('radio', { name: 'Skip' });
    expect((skip as HTMLInputElement).checked).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'Import 2' }));
    await waitFor(() =>
      expect(importCredentials).toHaveBeenLastCalledWith('org-1', {
        format: 'yaml',
        text: yaml,
        on_conflict: 'skip',
        dry_run: false,
      }),
    );
  });

  it('replaces the existing name when Replace is chosen', async () => {
    vi.mocked(importCredentials).mockResolvedValue(found);
    renderNew({ panel: 'import' });
    fireEvent.change(await screen.findByLabelText('Paste .env or YAML'), { target: { value: yaml } });
    await screen.findByText('3 found');

    fireEvent.click(screen.getByRole('radio', { name: 'Replace' }));
    fireEvent.click(screen.getByRole('button', { name: 'Import 3' }));
    await waitFor(() =>
      expect(importCredentials).toHaveBeenLastCalledWith('org-1', {
        format: 'yaml',
        text: yaml,
        on_conflict: 'replace',
        dry_run: false,
      }),
    );
  });

  it('keeps the pasted text out of the page, the URL and the caches after the import', async () => {
    vi.mocked(importCredentials).mockResolvedValue(found);
    const { router, queryClient } = renderNew({ panel: 'import' });
    fireEvent.change(await screen.findByLabelText('Paste .env or YAML'), { target: { value: yaml } });
    await screen.findByText('3 found');

    fireEvent.click(screen.getByRole('button', { name: 'Import 2' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/credentials'));
    expect(document.body.textContent).not.toContain('sntrys_test-1');
    expect(router.state.location.href).not.toContain('sntrys_test-1');
    for (const query of queryClient.getQueryCache().getAll()) {
      expect(JSON.stringify(query.state.data ?? null)).not.toContain('sntrys_test-1');
    }
  });

  it('renders the paste area masked', async () => {
    renderNew({ panel: 'import' });
    const paste = await screen.findByLabelText('Paste .env or YAML');
    expect(paste.className).toContain('[-webkit-text-security:disc]');
    expect(within(paste.parentElement as HTMLElement).queryByText(/sntrys_test/)).toBeNull();
  });
});
