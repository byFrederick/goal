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
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import {
  connectionKindsQuery,
  connectionsQuery,
  factoriesQuery,
  knowledgeDocumentsQuery,
  knowledgeSourceQuery,
  orgQuery,
} from '../../api/queries';
import type { KnowledgeSource, Org, OrgSummary } from '../../api/types';
import { TopBarProbe } from '../../test/top-bar-probe';
import { NewSourcePage } from './new-source-page';
import { SourcePage } from './source-page';

afterEach(() => {
  cleanup();
  delete (window as { matchMedia?: unknown }).matchMedia;
});

const summary: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 0, role: 'owner' };

const source = {
  id: 'src-1',
  name: 'eng-docs',
  title: 'Engineering docs',
  kind: 'upload',
  status: 'idle',
  created_by: { kind: 'user', id: 'u1', name: 'Ada' },
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  config: {},
  sync_every_seconds: 3600,
  documents: 0,
  used_by: { factories: [] },
} as unknown as KnowledgeSource;

function onPhone() {
  window.matchMedia = ((query: string) =>
    ({
      matches: query === '(max-width: 767px)',
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
    }) as unknown as MediaQueryList) as typeof window.matchMedia;
}

function renderAt(url: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const org = { ...summary, created_at: '2026-09-01T00:00:00Z', can: ['knowledge:manage'] } as Org;
  client.setQueryData(orgQuery(org.id).queryKey, org);
  client.setQueryData(factoriesQuery(org.id).queryKey, []);
  client.setQueryData(connectionsQuery(org.id).queryKey, []);
  client.setQueryData(connectionKindsQuery(org.id).queryKey, []);
  client.setQueryData(knowledgeSourceQuery(org.id, 'eng-docs').queryKey, source);
  client.setQueryData(knowledgeDocumentsQuery(org.id, 'eng-docs').queryKey, {
    pages: [{ documents: [] }],
    pageParams: [undefined],
  });

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org: summary }),
    component: Outlet,
  });
  const layout = createRoute({ getParentRoute: () => orgRoute, id: '_org', component: Outlet });
  const knowledge = createRoute({ getParentRoute: () => layout, path: 'knowledge', component: Outlet });
  const added = createRoute({
    getParentRoute: () => knowledge,
    path: 'new',
    component: () => (
      <TopBarProbe>
        <NewSourcePage />
      </TopBarProbe>
    ),
  });
  const one = createRoute({
    getParentRoute: () => knowledge,
    path: '$source',
    component: () => (
      <TopBarProbe>
        <SourcePage />
      </TopBarProbe>
    ),
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      orgRoute.addChildren([layout.addChildren([knowledge.addChildren([added, one])])]),
    ]),
    history: createMemoryHistory({ initialEntries: [url] }),
  });
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

describe('a knowledge source page', () => {
  it('has no in-page breadcrumb, and its top bar crumb is the source’s title, not its name', async () => {
    renderAt('/o/acme/knowledge/eng-docs');
    await screen.findByRole('heading', { level: 1, name: /Engineering docs/ });
    expect(screen.queryByRole('navigation', { name: 'Breadcrumb' })).toBeNull();
    const bar = screen.getByRole('region', { name: 'Top bar' });
    expect(bar.textContent).toBe('Engineering docs');
  });

  it('gives a phone Back to Knowledge in the top bar', async () => {
    onPhone();
    renderAt('/o/acme/knowledge/eng-docs');
    const back = await screen.findByRole('link', { name: 'Back to Knowledge' });
    expect(back.getAttribute('href')).toBe('/o/acme/knowledge');
  });
});

describe('the new knowledge source page', () => {
  it('has no in-page breadcrumb, and its top bar crumb is New source', async () => {
    renderAt('/o/acme/knowledge/new');
    await screen.findByRole('heading', { level: 1, name: 'New source' });
    expect(screen.queryByRole('navigation', { name: 'Breadcrumb' })).toBeNull();
    expect(screen.getByRole('region', { name: 'Top bar' }).textContent).toBe('New source');
  });

  it('gives a phone Back to Knowledge in the top bar', async () => {
    onPhone();
    renderAt('/o/acme/knowledge/new');
    const back = await screen.findByRole('link', { name: 'Back to Knowledge' });
    expect(back.getAttribute('href')).toBe('/o/acme/knowledge');
  });
});
