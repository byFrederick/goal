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
import { type ReactNode, Suspense, useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createKnowledgeSource } from '../../api/knowledge';
import {
  factoriesQuery,
  knowledgeDocumentsQuery,
  knowledgeSourceQuery,
  knowledgeSourcesQuery,
  orgQuery,
  secretsQuery,
} from '../../api/queries';
import type { KnowledgeDocument, KnowledgeSource, Org, OrgSummary, Secret } from '../../api/types';
import '../../test/select';
import { TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { TooltipProvider } from '../../ui/tooltip';
import { NewSourcePage } from './new-source-page';
import { SourcePage } from './source-page';

vi.mock('../../api/knowledge', async (original) => ({
  ...(await original<typeof import('../../api/knowledge')>()),
  createKnowledgeSource: vi.fn(),
}));

const secrets = [
  { name: 'github-token', scope: { all_factories: true, factories: [] } },
  { name: 'limited-key', scope: { all_factories: false, factories: ['f1'] } },
] as unknown as Secret[];

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const summary: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 0, role: 'owner' };

const synced = {
  id: 'src-1',
  name: 'eng-docs',
  title: 'Engineering docs',
  kind: 'synced',
  secret: 'github-token',
  status: 'idle',
  config: { reader: 'gitdocs', repository: 'https://github.com/acme/goal', branch: 'main', include: 'docs/**/*.md' },
  sync_every_seconds: 3600,
  last_synced_at: '2026-10-27T14:00:00Z',
  next_sync_at: '2026-10-27T15:00:00Z',
  documents: 1,
  created_by: { kind: 'user', id: 'u1', name: 'Ada' },
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  used_by: { factories: [] },
} as unknown as KnowledgeSource;

const doc = {
  id: 'd1',
  source: 'eng-docs',
  title: 'Deploying to production',
  url: 'https://github.com/acme/goal/blob/main/docs/deploy/production.md',
  mime: 'text/markdown',
  bytes: 12_288,
  status: 'indexed',
  indexed_at: '2026-10-27T14:02:00Z',
} as KnowledgeDocument;

function TopBarHost({ children }: { children: ReactNode }) {
  const [top, setTop] = useState<TopBarState | null>(null);
  return (
    <TopBarSetContext.Provider value={setTop}>
      <div data-testid="top-bar">{top?.actions}</div>
      {children}
    </TopBarSetContext.Provider>
  );
}

function renderPage(url: string, documents: KnowledgeDocument[] = [doc]) {
  const org: Org = { ...summary, created_at: '2026-09-01T00:00:00Z', can: ['knowledge:manage'] } as Org;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(orgQuery(org.id).queryKey, org);
  client.setQueryData(factoriesQuery(org.id).queryKey, []);
  client.setQueryData(knowledgeSourceQuery(org.id, 'eng-docs').queryKey, synced);
  client.setQueryData(knowledgeSourcesQuery(org.id).queryKey, [synced]);
  client.setQueryData(secretsQuery(org.id).queryKey, secrets);
  client.setQueryData(knowledgeDocumentsQuery(org.id, 'eng-docs').queryKey, {
    pages: [{ documents }],
    pageParams: [undefined],
  });
  const root = createRootRoute({
    component: () => (
      <TopBarHost>
        <Suspense fallback={null}>
          <Outlet />
        </Suspense>
      </TopBarHost>
    ),
  });
  const orgRoute = createRoute({
    getParentRoute: () => root,
    path: '/o/$org',
    beforeLoad: () => ({ org: summary }),
    component: Outlet,
  });
  const layout = createRoute({ getParentRoute: () => orgRoute, id: '_org', component: Outlet });
  const knowledge = createRoute({ getParentRoute: () => layout, path: 'knowledge', component: Outlet });
  const fresh = createRoute({ getParentRoute: () => knowledge, path: 'new', component: NewSourcePage });
  const source = createRoute({ getParentRoute: () => knowledge, path: '$source', component: SourcePage });
  const router = createRouter({
    routeTree: root.addChildren([orgRoute.addChildren([layout.addChildren([knowledge.addChildren([fresh, source])])])]),
    history: createMemoryHistory({ initialEntries: [url] }),
  });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe('a Knowledge source page', () => {
  it('has one crumb, a page-title heading and Sync now in the top bar', async () => {
    renderPage('/o/acme/knowledge/eng-docs');
    const h1 = await screen.findByRole('heading', { level: 1 });
    expect(h1.className).toContain('text-page-title');
    expect(screen.queryByRole('navigation', { name: 'Breadcrumb' })).toBeNull();
    const bar = screen.getByTestId('top-bar');
    expect(within(bar).getByRole('button', { name: 'Sync now, Engineering docs' })).toBeTruthy();
  });

  it('lists a document as its title, mono path, dot status, size and a bare icon button', async () => {
    renderPage('/o/acme/knowledge/eng-docs');
    const list = await screen.findByRole('list', { name: 'Documents of Engineering docs' });
    const row = within(list).getByRole('listitem');
    const title = within(row).getByRole('link', { name: 'Deploying to production' });
    // MASTER, Phones: the title answers across 44 px on a phone.
    expect(title.className).toContain('max-md:min-h-11');
    expect(title.className).not.toContain('max-md:-my-3');
    const path = within(row).getByText('docs/deploy/production.md');
    expect(path.className).toContain('font-mono');
    expect(path.getAttribute('title')).toBe('docs/deploy/production.md');
    expect(within(row).getByText(/^Indexed /)).toBeTruthy();
    expect(within(row).getByText('12 KiB')).toBeTruthy();
    const open = within(row).getByRole('link', { name: 'Open Deploying to production at its source' });
    expect(open.className).toContain('bg-transparent');
  });
});

describe('the document rows', () => {
  it('carry their full path in a title attribute and truncate it', async () => {
    const paths = [
      'docs/engineering/frontend.md',
      'docs/adr/0036-motion-rules-tokens-and-the-catalogue-of-transitions-for-every-screen.md',
      'docs/design/screens/knowledge/a-source-with-a-very-long-folder-name/index.md',
    ];
    const documents = paths.map((path, i) => ({
      ...doc,
      id: `d${i}`,
      title: `Document ${i}`,
      url: `https://github.com/acme/goal/blob/main/${path}`,
    })) as KnowledgeDocument[];
    renderPage('/o/acme/knowledge/eng-docs', documents);
    const list = await screen.findByRole('list', { name: 'Documents of Engineering docs' });
    const rows = within(list).getAllByRole('listitem');
    expect(rows).toHaveLength(paths.length);
    rows.forEach((row, i) => {
      const path = within(row).getByText(paths[i] as string);
      expect(path.getAttribute('title')).toBe(paths[i]);
      expect(path.className).toContain('truncate');
    });
  });
});

describe('a synced source', () => {
  it('names the secret it reads with and no connection', async () => {
    renderPage('/o/acme/knowledge/eng-docs');
    const link = await screen.findByRole('link', { name: 'github-token' });
    expect(link.getAttribute('href')).toBe('/o/acme/secrets/github-token');
    expect(screen.getByRole('combobox', { name: 'Secret' }).textContent).toContain('github-token');
    expect(screen.queryByText(/connection/i)).toBeNull();
  });
});

describe('the New source page', () => {
  it('picks a secret open to all factories for a synced source and sends it, not a connection', async () => {
    vi.mocked(createKnowledgeSource).mockRejectedValue(new Error('stop here'));
    renderPage('/o/acme/knowledge/new?kind=synced');
    await screen.findByRole('button', { name: 'Add source' });
    expect(screen.queryByText(/connection/i)).toBeNull();

    fireEvent.keyDown(screen.getByRole('combobox', { name: 'Secret' }), { key: 'Enter' });
    const list = await screen.findByRole('listbox');
    expect(within(list).queryByRole('option', { name: 'limited-key' })).toBeNull();
    fireEvent.click(within(list).getByRole('option', { name: 'github-token' }));

    fireEvent.change(screen.getByLabelText('Repository'), { target: { value: 'https://github.com/acme/docs' } });
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Docs' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add source' }));

    await waitFor(() => expect(createKnowledgeSource).toHaveBeenCalled());
    const body = vi.mocked(createKnowledgeSource).mock.calls[0]?.[1];
    expect(body).toMatchObject({ kind: 'synced', secret: 'github-token' });
    expect(body).not.toHaveProperty('connection');
  });

  it('shows Kind as a radio group of three cards, and Cancel and Add source inside the form', async () => {
    renderPage('/o/acme/knowledge/new');
    const group = await screen.findByRole('group', { name: 'Kind' });
    expect(within(group).getAllByRole('radio')).toHaveLength(3);
    expect(screen.queryByRole('navigation', { name: 'Breadcrumb' })).toBeNull();
    expect(screen.getByRole('heading', { level: 1, name: 'New source' }).className).toContain('text-page-title');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Add source' })).toBeTruthy());
    const form = screen.getByRole('button', { name: 'Add source' }).closest('form');
    expect(form).not.toBeNull();
    expect(within(form as HTMLElement).getByRole('link', { name: 'Cancel' })).toBeTruthy();
  });
});
