// A router harness for the Secrets tests: the list, a secret's page and Import mounted under the
// organization's secrets route, with the query data a test seeds so nothing else leaves the test.
// Used by tests only.
import { QueryClient, QueryClientProvider, type QueryKey } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { render } from '@testing-library/react';
import { useState } from 'react';
import { factoriesQuery, orgQuery, secretActivityQuery, secretQuery, secretsQuery } from '../../api/queries';
import type { AuditEntry, Factory, Org, OrgSummary, Secret } from '../../api/types';
import { TopBar, TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { TooltipProvider } from '../../ui/tooltip';
import { ImportSecretsPage } from './import-secrets-page';
import { secretsSearch } from './search';
import { SecretPage } from './secret-page';
import { SecretsPage } from './secrets-page';

export const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 3, role: null };
export const factories = [
  { id: 'f-software', name: 'software', title: 'Software' },
  { id: 'f-sre', name: 'sre', title: 'SRE' },
  { id: 'f-support', name: 'support', title: 'Support' },
] as unknown as Factory[];

export const NOW = Date.parse('2026-10-27T12:00:00Z');
export const DAY = 86_400_000;
export const iso = (ms: number) => new Date(ms).toISOString();
const by = { kind: 'user' as const, id: 'u', name: 'Freddy Cruz' };

export const secret = (over: Partial<Secret> & { name: string }): Secret => ({
  id: `id-${over.name}`,
  source: 'value',
  description: '',
  scope: { all_factories: true, factories: [] },
  expires_at: null,
  remind_days: 0,
  last_used_at: null,
  uses: [],
  created_by: by,
  created_at: iso(NOW - 10 * DAY),
  updated_by: by,
  updated_at: iso(NOW - 3 * DAY),
  ...over,
});

function TopBarFrame() {
  const [state, setState] = useState<TopBarState | null>(null);
  return (
    <TopBarSetContext.Provider value={setState}>
      <TopBar state={state} />
      <Outlet />
    </TopBarSetContext.Provider>
  );
}

/** Renders the Secrets screens at `at` (a path under /o/acme). `list` seeds the list, `entries` a secret's activity. */
export function renderSecrets({
  at = '/secrets',
  can = ['secret:read', 'secret:manage', 'audit:read'],
  list = [],
  entries,
}: {
  at?: string;
  can?: string[];
  list?: Secret[];
  entries?: Record<string, AuditEntry[]>;
} = {}) {
  window.scrollTo = () => {};
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const grants = { ...org, created_at: '2026-09-01T00:00:00Z', can, role: 'owner' } as unknown as Org;
  const seed: (readonly [QueryKey, unknown])[] = [
    [orgQuery(org.id).queryKey, grants],
    [factoriesQuery(org.id).queryKey, factories],
    [secretsQuery(org.id).queryKey, list],
    ...list.map((s) => [secretQuery(org.id, s.name).queryKey, s] as const),
    ...list.map(
      (s) =>
        [secretActivityQuery(org.id, s.id).queryKey, { entries: entries?.[s.name] ?? [], next_before: null }] as const,
    ),
  ];
  for (const [key, data] of seed) queryClient.setQueryData(key, data);

  const root = createRootRoute({ component: TopBarFrame });
  const orgRoute = createRoute({
    getParentRoute: () => root,
    path: '/o/$org',
    beforeLoad: () => ({ org }),
    component: Outlet,
  });
  const orgPages = createRoute({ getParentRoute: () => orgRoute, id: '_org', component: Outlet });
  const secrets = createRoute({ getParentRoute: () => orgPages, path: 'secrets', component: Outlet });
  const index = createRoute({
    getParentRoute: () => secrets,
    path: '/',
    validateSearch: (s: Record<string, unknown>) => secretsSearch(s),
    component: SecretsPage,
  });
  const one = createRoute({ getParentRoute: () => secrets, path: '$secret', component: SecretPage });
  const bulk = createRoute({ getParentRoute: () => secrets, path: 'import', component: ImportSecretsPage });
  const router = createRouter({
    routeTree: root.addChildren([
      orgRoute.addChildren([orgPages.addChildren([secrets.addChildren([index, one, bulk])])]),
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
    queryClient,
  };
}

/** Everything a test checks for a leaked value: the page, the URL, every query key and every cached query and mutation. */
export function leaks(value: string, { router, queryClient }: ReturnType<typeof renderSecrets>): string[] {
  const where: [string, string][] = [
    ['the page', document.body.innerHTML + document.body.textContent],
    ['the URL', router.state.location.href],
    [
      'a query',
      JSON.stringify(
        queryClient
          .getQueryCache()
          .getAll()
          .map((q) => [q.queryKey, q.state.data]),
      ),
    ],
    [
      'a mutation',
      JSON.stringify(
        queryClient
          .getMutationCache()
          .getAll()
          .map((m) => [m.options.mutationKey, m.state]),
      ),
    ],
  ];
  return where.filter(([, text]) => text.includes(value)).map(([name]) => name);
}
