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
import { getAccess } from '../../api/access';
import { accessQuery, inboxQuery, orgQuery, secretsQuery } from '../../api/queries';
import type { Access, AccessRow, AccessUse, AccessView, Org, OrgSummary, Secret } from '../../api/types';
import { TopBarSetContext } from '../../app/top-bar';
import { TooltipProvider } from '../../ui/tooltip';
import { AccessPage } from './access-page';
import { accessSearch } from './search';

vi.mock('../../api/access', () => ({ getAccess: vi.fn() }));

const NOW = Date.parse('2026-10-31T12:00:00Z');
const DAY = 86_400_000;
const iso = (ms: number) => new Date(ms).toISOString();
const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 3, role: null };

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
  window.scrollTo = () => {};
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.clearAllMocks();
});

const use = (factory: string, environment: string, status: AccessUse['status'] = 'ok'): AccessUse => ({
  factory: { id: `f-${factory}`, name: factory },
  environment,
  agents: ['builder'],
  status,
});
const row = (over: Partial<AccessRow> & Pick<AccessRow, 'kind' | 'name'>): AccessRow => ({
  uses: [use('software', 'default')],
  secrets: [],
  status: 'ok',
  ...over,
});

const rows = {
  linear: row({
    kind: 'mcp',
    name: 'linear',
    secrets: ['linear-key'],
    status: 'tested',
    tested_at: iso(NOW - 120_000),
  }),
  playwright: row({
    kind: 'mcp',
    name: 'playwright',
    status: 'tested',
    tested_at: iso(NOW - 120_000),
    uses: [use('software', 'default'), use('support', 'default')],
  }),
  ghToken: row({
    kind: 'env',
    name: 'GH_TOKEN',
    secrets: ['github-token'],
    last_used_at: iso(NOW - 120_000),
    uses: [use('software', 'default'), use('sre', 'default')],
  }),
  cloudflare: row({
    kind: 'env',
    name: 'CLOUDFLARE_API_TOKEN',
    secrets: ['cloudflare-deploy'],
    status: 'expiring',
    expires_at: iso(NOW + 3 * DAY),
    uses: [use('software', 'deploy', 'expiring')],
  }),
  zendesk: row({
    kind: 'env',
    name: 'ZENDESK_TOKEN',
    secrets: ['zendesk-token'],
    status: 'failed',
    test_error: '401',
    uses: [use('support', 'default', 'failed')],
  }),
  kube: row({
    kind: 'file',
    name: '~/.kube/config',
    secrets: ['kube-staging'],
    last_used_at: iso(NOW - 2 * DAY),
    uses: [use('software', 'deploy')],
  }),
};

const reach = (): Access => ({
  by: 'reach',
  reached: 6,
  need_attention: 2,
  groups: [
    { key: 'mcp', title: 'MCP servers', rows: [rows.linear, rows.playwright] },
    {
      key: 'env',
      title: 'Keys in variables and files',
      rows: [rows.ghToken, rows.cloudflare, rows.zendesk, rows.kube],
    },
  ],
});
const byFactory = (): Access => ({
  ...reach(),
  by: 'factory',
  groups: [
    { key: 'f-software', title: 'software', rows: [rows.linear, rows.playwright, rows.ghToken, rows.cloudflare] },
    { key: 'f-support', title: 'support', rows: [rows.zendesk] },
  ],
});
const bySecret = (): Access => ({
  ...reach(),
  by: 'secret',
  groups: [
    { key: 'github-token', title: 'github-token', rows: [rows.ghToken] },
    { key: 'zendesk-token', title: 'zendesk-token', rows: [rows.zendesk] },
  ],
});

const secrets = [
  { name: 'cloudflare-deploy', expires_at: iso(NOW + 3 * DAY) },
  { name: 'github-token', expires_at: null },
] as unknown as Secret[];

type Seeded = Partial<Record<AccessView, Access>>;

function renderAccess({
  at = '/access',
  seeded = { reach: reach(), factory: byFactory(), secret: bySecret() },
  approvals = 2,
}: {
  at?: string;
  seeded?: Seeded;
  approvals?: number;
} = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const grants = { ...org, created_at: '2026-09-01T00:00:00Z', can: ['access:read'], role: 'owner' } as unknown as Org;
  const entries = Array.from({ length: approvals }, (_, i) => ({ id: `e${i}`, tool_approval: { agent: 'builder' } }));
  const seed: (readonly [QueryKey, unknown])[] = [
    [orgQuery(org.id).queryKey, grants],
    [secretsQuery(org.id).queryKey, secrets],
    [inboxQuery(org.id).queryKey, { entries: [...entries, { id: 'item' }], truncated: false }],
    ...(Object.entries(seeded) as [AccessView, Access][]).map(
      ([by, data]) => [accessQuery(org.id, by).queryKey, data] as const,
    ),
  ];
  for (const [key, data] of seed) queryClient.setQueryData(key, data);

  const root = createRootRoute({
    component: () => (
      <TopBarSetContext.Provider value={() => {}}>
        <Outlet />
      </TopBarSetContext.Provider>
    ),
  });
  const orgRoute = createRoute({
    getParentRoute: () => root,
    path: '/o/$org',
    beforeLoad: () => ({ org }),
    component: Outlet,
  });
  const page = createRoute({
    getParentRoute: () => orgRoute,
    path: 'access',
    validateSearch: (s: Record<string, unknown>) => accessSearch(s),
    component: AccessPage,
  });
  const stub = (path: string) => createRoute({ getParentRoute: () => orgRoute, path, component: () => <p>{path}</p> });
  const router = createRouter({
    routeTree: root.addChildren([
      orgRoute.addChildren([page, stub('needs-you'), stub('secrets/$secret'), stub('f/$factory/definition/$section')]),
    ]),
    history: createMemoryHistory({ initialEntries: [`/o/acme${at}`] }),
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
  };
}

describe('the Access page', () => {
  it('shows the stat strip: what is reached, what needs attention and the approvals waiting', async () => {
    renderAccess();

    expect(await screen.findByRole('heading', { name: 'Access' })).toBeTruthy();
    expect(screen.getAllByText('Reached')).toHaveLength(2);
    expect(screen.getByText('2 MCP servers and 4 keys, in 4 environments')).toBeTruthy();
    expect(screen.getByText('A key expires, a test fails')).toBeTruthy();
    expect(screen.getByText('Waiting for your OK')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Review in Needs you' }).getAttribute('href')).toBe('/o/acme/needs-you');
    const dt = screen.getByText('Waiting for your OK');
    expect(dt.closest('div')?.textContent).toContain('2');
  });

  it('names the soonest expiring secret and links to replace it', async () => {
    renderAccess();

    const alert = await screen.findByRole('region', { name: 'Expiring' });
    expect(within(alert).getByText('cloudflare-deploy expires in 3 days')).toBeTruthy();
    expect(within(alert).getByText('software · deploy reads it as CLOUDFLARE_API_TOKEN.')).toBeTruthy();
    expect(within(alert).getByRole('link', { name: 'Replace value' }).getAttribute('href')).toBe(
      '/o/acme/secrets/cloudflare-deploy',
    );
  });

  it('shows the matrix by what they reach: groups, a column per environment, each row’s key and status', async () => {
    renderAccess();

    const table = await screen.findByRole('table');
    expect(within(table).getByText('MCP servers')).toBeTruthy();
    expect(within(table).getByText('Keys in variables and files')).toBeTruthy();
    for (const column of ['softwaredefault', 'softwaredeploy', 'sredefault', 'supportdefault']) {
      expect(
        within(table)
          .getAllByRole('columnheader')
          .some((h) => h.textContent === column),
      ).toBe(true);
    }
    const linear = within(table).getByText('linear').closest('tr') as HTMLElement;
    expect(within(linear).getByText('linear-key')).toBeTruthy();
    expect(within(linear).getByText('Tested 2 min ago')).toBeTruthy();
    expect(within(linear).getAllByText('–')).toHaveLength(3);
    const playwright = within(table).getByText('playwright').closest('tr') as HTMLElement;
    expect(within(playwright).getByText('No key')).toBeTruthy();
    const kube = within(table).getByText('~/.kube/config').closest('tr') as HTMLElement;
    expect(within(kube).getByText('Used Oct 29')).toBeTruthy();
  });

  it('marks a failed test with its error and an expiring key with its days', async () => {
    renderAccess();

    const table = await screen.findByRole('table');
    expect(within(table).getByText('Test failed: 401')).toBeTruthy();
    expect(within(table).getByText('Expires in 3 days')).toBeTruthy();
  });

  it('lists the same rows with their environments as chips for a narrow screen', async () => {
    renderAccess();

    const mcp = await screen.findByRole('region', { name: 'MCP servers' });
    const playwright = within(mcp).getByText('playwright').closest('li') as HTMLElement;
    expect(within(playwright).getByRole('link', { name: 'software · default' })).toBeTruthy();
    expect(within(playwright).getByRole('link', { name: 'support · default' })).toBeTruthy();
  });

  it('opens the environment’s file in the factory’s definition from a row', async () => {
    const { router } = renderAccess();

    const table = await screen.findByRole('table');
    fireEvent.click(within(table).getByRole('link', { name: 'CLOUDFLARE_API_TOKEN' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/software/definition/basics'));
    expect(router.state.location.search).toEqual({ mode: 'yaml', file: 'environments/deploy.yaml' });
  });

  it('switches to By factory and By secret, keeping the view in the URL', async () => {
    const { router } = renderAccess();

    fireEvent.click(await screen.findByRole('radio', { name: 'By factory' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ by: 'factory' }));
    const table = await screen.findByRole('table');
    expect(within(table).getByText('software', { selector: 'th' })).toBeTruthy();
    expect(within(table).getByText('support', { selector: 'th' })).toBeTruthy();

    fireEvent.click(screen.getByRole('radio', { name: 'By secret' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ by: 'secret' }));
    expect(within(await screen.findByRole('table')).getByText('github-token', { selector: 'th' })).toBeTruthy();

    fireEvent.click(screen.getByRole('radio', { name: 'By what they reach' }));
    await waitFor(() => expect(router.state.location.search).toEqual({}));
  });

  it('opens on the view in the URL', async () => {
    renderAccess({ at: '/access?by=secret' });

    expect(((await screen.findByRole('radio', { name: 'By secret' })) as HTMLInputElement).checked).toBe(true);
  });

  it('filters the rows by the search text kept in the URL', async () => {
    renderAccess({ at: '/access?q=zendesk' });

    const table = await screen.findByRole('table');
    expect(within(table).getByText('ZENDESK_TOKEN')).toBeTruthy();
    expect(within(table).queryByText('linear')).toBeNull();
  });

  it('says what to do when no agent reaches anything', async () => {
    renderAccess({ seeded: { reach: { by: 'reach', reached: 0, need_attention: 0, groups: [] } } });

    expect(await screen.findByText('No agent reaches a tool or a key yet')).toBeTruthy();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('shows no alert when nothing expires', async () => {
    renderAccess({
      seeded: {
        reach: {
          by: 'reach',
          reached: 1,
          need_attention: 0,
          groups: [{ key: 'mcp', title: 'MCP servers', rows: [rows.linear] }],
        },
      },
    });

    await screen.findByRole('table');
    expect(screen.queryByRole('region', { name: 'Expiring' })).toBeNull();
    expect(screen.getByText('Everything is in order')).toBeTruthy();
  });

  it('keeps its header and offers Try again when the read model fails', async () => {
    vi.mocked(getAccess).mockRejectedValue(new Error('down'));
    renderAccess({ seeded: {} });

    expect(await screen.findByText('Couldn’t load access. Check your connection and try again.')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Access' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('shows its skeleton, said as Loading, while the read model loads', async () => {
    vi.mocked(getAccess).mockReturnValue(new Promise(() => {}));
    renderAccess({ seeded: {} });

    const loading = await screen.findByText('Loading');
    expect(loading.parentElement?.getAttribute('aria-busy')).toBe('true');
    expect(screen.queryByRole('table')).toBeNull();
  });
});
