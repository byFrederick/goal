// Mounts the spend screen under an organization's route with a memory history at `url`, and a query
// client without retries. The screen reads the organization from the route context and the window from
// the search. Used by tests only.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { render } from '@testing-library/react';
import { type ReactNode, useState } from 'react';
import type { OrgSummary } from '../../api/types';
import { TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { TooltipProvider } from '../../ui/tooltip';
import { spendSearch } from './search';
import { SpendPage } from './spend-page';

export const acme: OrgSummary = {
  id: 'org-1',
  slug: 'acme',
  name: 'Acme',
  mark: 'Ac',
  factory_count: 3,
  role: 'owner',
};

/** Stands in for the shell's top bar: the page's actions render in a header, as they do on screen. */
function TopBarHarness({ children }: { children: ReactNode }) {
  const [bar, setBar] = useState<TopBarState | null>(null);
  return (
    <TopBarSetContext.Provider value={setBar}>
      <header data-testid="top-bar">{bar?.actions}</header>
      {children}
    </TopBarSetContext.Provider>
  );
}

export function renderSpend(url = '/o/acme/spend') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute();
  const org = createRoute({
    getParentRoute: () => root,
    path: '/o/$org',
    beforeLoad: () => ({ org: acme }),
  });
  const spend = createRoute({
    getParentRoute: () => org,
    path: 'spend',
    validateSearch: (search: Record<string, unknown>) => spendSearch(search),
    component: SpendPage,
  });
  const router = createRouter({
    routeTree: root.addChildren([org.addChildren([spend])]),
    history: createMemoryHistory({ initialEntries: [url] }),
    context: { queryClient },
  });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <TopBarHarness>
          <RouterProvider router={router} />
        </TopBarHarness>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { ...view, router };
}
