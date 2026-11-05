// A router harness for the Webhooks tests: the list, a webhook's page and New webhook mounted under the
// organization's webhooks route, with the query data a test seeds. Used by tests only.
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
import { factoriesQuery, orgQuery, secretsQuery, webhookQuery, webhooksQuery } from '../../api/queries';
import type { Factory, Org, OrgSummary, Secret } from '../../api/types';
import type { Webhook } from '../../api/webhooks';
import { TopBar, TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { TooltipProvider } from '../../ui/tooltip';
import { NewWebhookPage } from './new-webhook-page';
import { WebhookPage } from './webhook-page';
import { WebhooksPage } from './webhooks-page';

export const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 3, role: null };
export const factories = [
  { id: 'f-software', name: 'software', title: 'Software' },
  { id: 'f-sre', name: 'sre', title: 'SRE' },
  { id: 'f-support', name: 'support', title: 'Support' },
] as unknown as Factory[];

const today = (n: number) => ({ day: `2026-11-0${n}`, delivered: 0, refused: 0 });

export const webhook = (over: Partial<Webhook> & { name: string }): Webhook => ({
  id: `id-${over.name}`,
  sender: 'github',
  address: `/hooks/id-${over.name}`,
  secret: `webhook-${over.name}`,
  enabled: true,
  token: null,
  openings: [],
  triggers: [],
  created_at: '2026-10-01T00:00:00Z',
  days: [1, 2, 3, 4, 5, 6, 7].map(today),
  pinged_at: null,
  setup_events: [],
  setup_all_events: false,
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

/** Renders the Webhooks screens at `at` (a path under /o/acme). `list` seeds the list and each webhook's own read. */
export function renderWebhooks({
  at = '/webhooks',
  can = ['webhook:read', 'webhook:manage', 'secret:manage'],
  list = [],
  secrets = [],
}: {
  at?: string;
  can?: string[];
  list?: Webhook[];
  secrets?: Secret[];
} = {}) {
  window.scrollTo = () => {};
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const grants = { ...org, created_at: '2026-09-01T00:00:00Z', can, role: 'owner' } as unknown as Org;
  const seed: (readonly [QueryKey, unknown])[] = [
    [orgQuery(org.id).queryKey, grants],
    [factoriesQuery(org.id).queryKey, factories],
    [secretsQuery(org.id).queryKey, secrets],
    [webhooksQuery(org.id).queryKey, list],
    ...list.map((w) => [webhookQuery(org.id, w.name).queryKey, w] as const),
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
  const hooks = createRoute({ getParentRoute: () => orgPages, path: 'webhooks', component: Outlet });
  const index = createRoute({ getParentRoute: () => hooks, path: '/', component: WebhooksPage });
  const create = createRoute({ getParentRoute: () => hooks, path: 'new', component: NewWebhookPage });
  const one = createRoute({ getParentRoute: () => hooks, path: '$webhook', component: WebhookPage });
  const secretPage = createRoute({
    getParentRoute: () => orgPages,
    path: 'secrets/$secret',
    component: () => <p>A secret</p>,
  });
  const router = createRouter({
    routeTree: root.addChildren([
      orgRoute.addChildren([orgPages.addChildren([hooks.addChildren([index, create, one]), secretPage])]),
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
